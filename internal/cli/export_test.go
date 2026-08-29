package cli

// Opens the command constructors, the init wizard's flow classifier and the
// §7.1 output inspect/commit pair to the external test package. These are
// driven directly because the behaviour under test is the split between
// inspection and mutation, which a run through the assembled cobra command
// cannot observe halfway; running them from `package cli` put them, and the
// testify they assert with, outside govulncheck's call graph (bd gqlc-m5rc).
//
// This file declares no imports on purpose. `vuln-root-residual` measures a
// package's blindness from the imports of its in-package test files, so one
// third-party import here would put internal/cli back in the blind set and
// undo the conversion.
//
// targetPlan's fields stay unexported. This file is in package cli, so it can
// carry the constructor and accessor the external tests need, and production
// code does not change shape to suit a scanner.
type TargetPlan = targetPlan

func NewTargetPlan(dir string, create bool, wipe []string) targetPlan {
	return targetPlan{dir: dir, create: create, wipe: wipe}
}

func (p targetPlan) Wipe() []string { return p.wipe }

const (
	FlowFresh       = flowFresh
	FlowEdit        = flowEdit
	FlowBroken      = flowBroken
	NoCurrentDriver = noCurrentDriver
)

var (
	AddPrefill       = addPrefill
	ClassifyConfig   = classifyConfig
	CommitOutputs    = commitOutputs
	EpilogueText     = epilogueText
	InitDefaults     = initDefaults
	InspectOutputs   = inspectOutputs
	NewInitCmd       = newInitCmd
	NewRootCmd       = newRootCmd
	OfferableDrivers = offerableDrivers
	PreviewBlock     = previewBlock
	RunInit          = runInit
	RunInitAdd       = runInitAdd
	RunInitWizard    = runInitWizard
	ValidateOut      = validateOut
	ValidatePackage  = validatePackage
	WarningsText     = warningsText
)
