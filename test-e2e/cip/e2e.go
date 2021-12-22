/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v2"

	"sigs.k8s.io/promo-tools/v3/internal/version"
	reg "sigs.k8s.io/promo-tools/v3/legacy/dockerregistry"
	"sigs.k8s.io/promo-tools/v3/legacy/gcloud"
	"sigs.k8s.io/promo-tools/v3/legacy/stream"
	"sigs.k8s.io/release-utils/command"
)

const kpromoMain = "cmd/kpromo/main.go"

func main() {
	// NOTE: We can't run the tests in parallel because we only have 1 pair of
	// staging/prod GCRs.

	testsPtr := flag.String(
		"tests", "", "the e2e tests file (YAML) to load (REQUIRED)")
	repoRootPtr := flag.String(
		"repo-root", "", "the absolute path of the CIP git repository on disk")
	keyFilePtr := flag.String(
		"key-file", "", "the .json key file to use to activate the service account in the tests (tests only support using 1 service account)")
	helpPtr := flag.Bool(
		"help",
		false,
		"print help")

	flag.Parse()

	// Log linker flags
	printVersion()

	if len(os.Args) == 1 {
		printUsage()
		os.Exit(0)
	}

	if *helpPtr {
		printUsage()
		os.Exit(0)
	}

	if *repoRootPtr == "" {
		logrus.Fatalf("-repo-root=... flag is required")
	}

	ts, err := readE2ETests(*testsPtr)
	if err != nil {
		logrus.Fatal(err)
	}

	if len(*keyFilePtr) > 0 {
		if err := gcloud.ActivateServiceAccount(*keyFilePtr); err != nil {
			logrus.Fatalf("activating service account from .json: %q", err)
		}
	}

	// Loop through each e2e test case.
	for _, t := range ts {
		fmt.Printf("\n===> Running e2e test '%s'...\n", t.Name)
		// TODO(e2e): Potential nil pointer
		if err := testSetup(*repoRootPtr, &t); err != nil {
			logrus.Fatalf("error with test setup: %q", err)
		}

		fmt.Println("checking snapshots BEFORE promotion:")
		for _, snapshot := range t.Snapshots {
			if err := checkSnapshot(snapshot.Name, snapshot.Before, *repoRootPtr, t.Registries); err != nil {
				logrus.Fatalf("error checking snapshot before promotion for %s: %q", snapshot.Name, err)
			}
		}

		err = runPromotion(*repoRootPtr, &t)
		if err != nil {
			logrus.Fatalf("error with promotion: %q", err)
		}

		fmt.Println("checking snapshots AFTER promotion:")
		for _, snapshot := range t.Snapshots {
			err := checkSnapshot(snapshot.Name, snapshot.After, *repoRootPtr, t.Registries)
			if err != nil {
				logrus.Fatalf("error checking snapshot for %s: %q", snapshot.Name, err)
			}
		}

		fmt.Printf("\n===> e2e test '%s': OK\n", t.Name)
	}
}

func checkSnapshot(
	repo reg.RegistryName,
	expected []reg.Image,
	repoRoot string,
	rcs []reg.RegistryContext,
) error {
	got, err := getSnapshot(
		repoRoot,
		repo,
		rcs,
	)
	if err != nil {
		return errors.Wrapf(err, "getting snapshot of %s", repo)
	}

	diff := cmp.Diff(got, expected)
	if diff != "" {
		fmt.Printf("the following diff exists: %s", diff)
		return errors.Errorf("expected equivalent image sets")
	}

	return nil
}

func testSetup(repoRoot string, t *E2ETest) error {
	gcloudConfigCmd := command.New("gcloud", "config", "configurations", "list")

	logrus.Infof("executing %s", gcloudConfigCmd.String())

	configStream, err := gcloudConfigCmd.RunSuccessOutput()
	if err != nil {
		logrus.Errorf("%+v", err)
	}

	fmt.Println(configStream.Output())
	fmt.Println(configStream.Error())

	if err := t.clearRepositories(); err != nil {
		return errors.Wrap(err, "cleaning test repository")
	}

	goldenPush := fmt.Sprintf("%s/test-e2e/golden-images/push-golden.sh", repoRoot)

	cmd := command.NewWithWorkDir(
		repoRoot,
		goldenPush,
	)

	logrus.Infof("executing %s", cmd.String())

	std, err := cmd.RunSuccessOutput()
	// TODO(e2e): Potential nil pointer
	fmt.Println(std.Output())
	fmt.Println(std.Error())
	return err
}

func runPromotion(repoRoot string, t *E2ETest) error {
	args := []string{
		"run",
		fmt.Sprintf(
			"%s/%s",
			repoRoot,
			kpromoMain,
		),
		"cip",
		"--confirm",
		"--log-level=debug",
		"--use-service-account",
		// There is no need to use -key-files=... because we already activated
		// the 1 service account we need during e2e tests with our own -key-file
		// flag.
	}

	argsFinal := []string{}

	args = append(args, t.Invocation...)

	for _, arg := range args {
		argsFinal = append(argsFinal, strings.ReplaceAll(arg, "$PWD", repoRoot))
	}

	fmt.Println("execing cmd", "go", argsFinal)
	cmd := command.NewWithWorkDir(repoRoot, "go", argsFinal...)
	return cmd.RunSuccess()
}

func extractSvcAcc(registry reg.RegistryName, rcs []reg.RegistryContext) string {
	for _, r := range rcs {
		if r.Name == registry {
			return r.ServiceAccount
		}
	}

	return ""
}

func getSnapshot(
	repoRoot string,
	registry reg.RegistryName,
	rcs []reg.RegistryContext,
) ([]reg.Image, error) {
	// TODO: Consider setting flag names in `cip` instead
	invocation := []string{
		"run",
		fmt.Sprintf(
			"%s/%s",
			repoRoot,
			kpromoMain,
		),
		"cip",
		"--snapshot=" + string(registry),
	}

	svcAcc := extractSvcAcc(registry, rcs)
	if len(svcAcc) > 0 {
		invocation = append(invocation, "--snapshot-service-account="+svcAcc)
	}

	fmt.Println("execing cmd", "go", invocation)
	// TODO: Replace with sigs.k8s.io/release-utils/command once the package
	//       exposes a means to manipulate stdout.Bytes() for unmarshalling.
	cmd := exec.Command("go", invocation...)

	cmd.Dir = repoRoot

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		fmt.Println("stdout", stdout.String())
		fmt.Println("stderr", stderr.String())
		return nil, err
	}

	images := make([]reg.Image, 0)
	if err := yaml.UnmarshalStrict(stdout.Bytes(), &images); err != nil {
		return nil, err
	}

	return images, err
}

func (t *E2ETest) clearRepositories() error {
	// We need a SyncContext to clear the repos. That's it. The actual
	// promotions will be done by the cip binary, not this tool.
	sc, err := reg.MakeSyncContext(
		[]reg.Manifest{
			{Registries: t.Registries},
		},
		10,
		true,
		true)
	if err != nil {
		return errors.Wrap(err, "trying to create sync context")
	}

	sc.ReadRegistries(
		sc.RegistryContexts,
		// Read all registries recursively, because we want to delete every
		// image found in it (clearRepository works by deleting each image found
		// in sc.Inv).
		true,
		reg.MkReadRepositoryCmdReal)

	// Clear ALL registries in the test manifest. Blank slate!
	for _, rc := range t.Registries {
		fmt.Println("CLEARING REPO", rc.Name)
		clearRepository(rc.Name, &sc)
	}
	return nil
}

func clearRepository(regName reg.RegistryName, sc *reg.SyncContext) {
	mkDeletionCmd := func(
		dest reg.RegistryContext,
		imageName reg.ImageName,
		digest reg.Digest) stream.Producer {
		var sp stream.Subprocess
		sp.CmdInvocation = reg.GetDeleteCmd(
			dest,
			sc.UseServiceAccount,
			imageName,
			digest,
			true)
		return &sp
	}

	sc.ClearRepository(regName, mkDeletionCmd, nil)
}

// E2ETest holds all the information about a single e2e test. It has the
// promoter manifest, and the before/after snapshots of all repositories that it
// cares about.
type E2ETest struct {
	Name       string                `yaml:"name,omitempty"`
	Registries []reg.RegistryContext `yaml:"registries,omitempty"`
	Invocation []string              `yaml:"invocation,omitempty"`
	Snapshots  []RegistrySnapshot    `yaml:"snapshots,omitempty"`
}

// E2ETests is an array of E2ETest.
type E2ETests []E2ETest

// RegistrySnapshot is the snapshot of a registry. It is basically the key/value
// pair out of the reg.MasterInventory type (RegistryName + []Image).
type RegistrySnapshot struct {
	Name   reg.RegistryName `yaml:"name,omitempty"`
	Before []reg.Image      `yaml:"before,omitempty"`
	After  []reg.Image      `yaml:"after,omitempty"`
}

func readE2ETests(filePath string) (E2ETests, error) {
	var ts E2ETests
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return ts, err
	}
	if err := yaml.UnmarshalStrict(b, &ts); err != nil {
		return ts, err
	}

	return ts, nil
}

func printVersion() {
	logrus.Infof("%s", version.Get().String())
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}
