package liverecipes

// Arm is the live half a live test runs in. Which containers a test needs is
// a property of its body — it calls into the neo4j driver, or into the AGE
// dialect — and the union guard in split.go cannot see it: moving a test from
// the half that has its container to the other half keeps the union of the
// two -run lists unchanged, and dropping it from its own half while the other
// half still claims it does too. Both stay green there, so the arm is
// declared per test here and the recipes are checked against it
// (bd gqlc-vh74).
type Arm string

const (
	// ArmNeo4j is the PR-blocking half, against one neo4j:5-community image.
	ArmNeo4j Arm = "neo4j"
	// ArmAGE is the other PR-blocking half, each test on its own AGE
	// container. PR-blocking since bd gqlc-ezwae; it was nightly-and-manual
	// before that, and the halves still differ on merge_group. Which events
	// each half runs on is declared in ArmTriggers rather than here.
	ArmAGE Arm = "age"
	// ArmAll is a test that runs in both halves, with a -skip carving out the
	// other half's subtests. TestLiveSmoke is one by design: the smoke battery
	// has a subtest per backend, and each half runs its own.
	ArmAll Arm = "all"
)

// The justfile recipes each arm is read from. The names are the join key
// between the workflows that reach them and the bodies liverecipes.Read
// attributes to CI, so a rename here has to follow the justfile and the
// workflow together.
const (
	// Neo4jRecipe is the neo4j half CI runs.
	Neo4jRecipe = "test-codegen-live-neo4j"
	// AgeRecipe is the AGE half CI runs.
	AgeRecipe = "test-codegen-live-age"
)

// LiveArms is every live test the codegen module declares and the half it
// runs in.
//
// Update rule: a new live test gets a new row here, no exceptions. The test
// beside this file fails on a declared test with no row, so adding the test
// without the row is red from the start; picking the arm means reading the
// test's body for which container it needs, because the name cannot serve —
// AGE-prefixed and backend-neutral tests sit in the neo4j half by design, and
// a naming rule would misfile them.
var LiveArms = map[string]Arm{
	"TestLiveSmoke":                               ArmAll,
	"TestEveryBatteryIsTheDeclaredSize":           ArmNeo4j,
	"TestEveryBatteryIsNamedInScenarioTables":     ArmNeo4j,
	"TestTxMethodSet":                             ArmNeo4j,
	"TestNeo4jRefusesANestedListStoredProperty":   ArmNeo4j,
	"TestNeo4jRefusesAMapValuedStoredProperty":    ArmNeo4j,
	"TestAGERefusesAUint64ParameterAboveMaxInt64": ArmNeo4j,
	"TestAGESessionInit":                          ArmAGE,
	"TestAGERefusesRelationshipTypeAlternation":   ArmAGE,
	"TestAGERefusesTheFunctionsItDoesNotDefine":   ArmAGE,
	"TestAGERefusesTheSpatialConstructor":         ArmAGE,
	"TestAGERefusesTheNamespaceItHasNoSchemaFor":  ArmAGE,
	"TestAGEOffsetSidecar":                        ArmAGE,
	"TestAGEZonedTime":                            ArmAGE,
	"TestAGEStoresANestedListProperty":            ArmAGE,
	"TestAGEStoresARecordProperty":                ArmAGE,
}
