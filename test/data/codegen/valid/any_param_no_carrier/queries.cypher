// An ANY VALUE and a LIST<ANY VALUE> parameter in a package that declares
// none of gqlc's neutral carriers: no temporal property and no UUID one
// (bd gqlc-nvb4). fromAnyValue's arms may then name time.Time only, since
// an arm for Date or UUID would name a type this package does not declare
// and the package would not compile. TestGoldenBuild is what holds that;
// valid/any_param_timestamp is the fixture that declares the carriers.

// name: AddSlot :exec
CREATE (s:Slot {id: $id, payload: $payload, bag: $bag})
