package promoter

import (
	"github.com/pkg/errors"
	reg "sigs.k8s.io/promo-tools/v3/legacy/dockerregistry"
	"sigs.k8s.io/promo-tools/v3/legacy/stream"
)

var AllowedOutputFormats = []string{
	"csv",
	"yaml",
}

type Promoter struct {
	Options *Options
	impl    promoterImplementation
}

func New() *Promoter {
	return &Promoter{
		Options: DefaultOptions,
		impl:    &defaultPromoterImplementation{},
	}
}

// promoterImplementation
type promoterImplementation interface {
	// General methods common to all modes of the promoter
	ValidateOptions(*Options) error
	ActivateServiceAccounts(*Options) error

	// Methods for promotion mode:
	ParseManifests(*Options) ([]reg.Manifest, error)
	MakeSyncContext(*Options, []reg.Manifest) (*reg.SyncContext, error)
	GetPromotionEdges(*reg.SyncContext, []reg.Manifest) (map[reg.PromotionEdge]interface{}, error)
	MakeProducerFunction(bool) streamProducerFunc
	PromoteImages(*reg.SyncContext, map[reg.PromotionEdge]interface{}, streamProducerFunc) error

	// Methods for snapshot mode:
	GetSnapshotSourceRegistry(*Options) (*reg.RegistryContext, error)
	GetSnapshotManifests(*Options) ([]reg.Manifest, error)
	AppendManifestToSnapshot(*Options, []reg.Manifest) ([]reg.Manifest, error)
	GetRegistryImageInventory(*Options, []reg.Manifest) (reg.RegInvImage, error)
	Snapshot(*Options, reg.RegInvImage) error
}

// streamProducerFunc is a function that gets the required fields to
// construct a promotion stream producer
type streamProducerFunc func(
	srcRegistry reg.RegistryName, srcImageName reg.ImageName,
	destRC reg.RegistryContext, imageName reg.ImageName,
	digest reg.Digest, tag reg.Tag, tp reg.TagOp,
) stream.Producer

// PromoteImages is the main method for image promotion
// it runs by taking all its parameters from a set of options.
func (p *Promoter) PromoteImages(opts *Options) (err error) {
	// Validate the options. Perhaps another image-specific
	// validation function may be needed.
	if err := p.impl.ValidateOptions(opts); err != nil {
		return errors.Wrap(err, "validating options")
	}

	if err := p.impl.ActivateServiceAccounts(opts); err != nil {
		return errors.Wrap(err, "activating service accounts")
	}

	mfests, err := p.impl.ParseManifests(opts)
	if err != nil {
		return errors.Wrap(err, "parsing manifests")
	}

	sc, err := p.impl.MakeSyncContext(opts, mfests)
	if err != nil {
		return errors.Wrap(err, "creating sync context")
	}

	promotionEdges, err := p.impl.GetPromotionEdges(sc, mfests)
	if err != nil {
		return errors.Wrap(err, "filtering edges")
	}

	// MakeProducer
	producerFunc := p.impl.MakeProducerFunction(sc.UseServiceAccount)

	// If parseOnly from the original cli.Run fn is kept, this is where it goes

	return errors.Wrap(
		p.impl.PromoteImages(sc, promotionEdges, producerFunc),
		"running promotion",
	)
}

func (p *Promoter) ValidateManifestLists(opts *Options) error {
	// STUB
	return nil
}

// Snapshot runs the steps to output a representation in json or yaml of a registry
func (p *Promoter) Snapshot(opts *Options) (err error) {
	if err := p.impl.ValidateOptions(opts); err != nil {
		return errors.Wrap(err, "validating options")
	}

	if err := p.impl.ActivateServiceAccounts(opts); err != nil {
		return errors.Wrap(err, "activating service accounts")
	}

	mfests, err := p.impl.GetSnapshotManifests(opts)
	if err != nil {
		return errors.Wrap(err, "getting snapshot manifests")
	}

	mfests, err = p.impl.AppendManifestToSnapshot(opts, mfests)
	if err != nil {
		return errors.Wrap(err, "adding the specified manifest to the snapshot context")
	}

	rii, err := p.impl.GetRegistryImageInventory(opts, mfests)
	if err != nil {
		return errors.Wrap(err, "getting registry image inventory")
	}

	return errors.Wrap(p.impl.Snapshot(opts, rii), "generating snapshot")
}

func (p *Promoter) SecurityScan(opts *Options) error {
	// STUB
	return nil
}

func (p *Promoter) CheckManifestLists(opts *Options) error {
	// STUB
	return nil
}

type defaultPromoterImplementation struct{}
