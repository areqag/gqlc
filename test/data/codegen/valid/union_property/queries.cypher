// Both enrolled targets, unlike record_property's AGE-only enrolment, and
// the difference is a fact about the two stores rather than about the two
// kinds. A record is a map and neo4j will not hold a map in a property, so
// enrolling neo4j there would pin goldens for a storage claim nobody has
// measured. A closed union stores whatever its chosen member stores — an
// INT32 or a STRING here, both of which every enrolled server has always
// held — so there is no unmeasured storage claim to avoid, and running
// both targets is what makes the fixture say something the unit tests
// cannot: that the DECLARED surface does not vary by backend (`any` on
// both) while the emission behind it does.
//
// What the three union properties are for:
//
//   pick and also are ONE encoding at two sites. The emission is per
//   encoding rather than per position, so they share a single helper set
//   on each backend, and a golden that grew two sets is the regression.
//   record_property makes this point across the two NULLABILITIES; no
//   union fixture can, because neither closed-union alternative admits a
//   notNull of its own (GQL.g4:1731-1732), so a trailing NOT NULL is a
//   syntax error after `>` and binds to the last MEMBER when written
//   inside. Every closed-union property is therefore nullable, and two
//   sites is the whole of the axis that is left.
//
//   flag is a SECOND, distinct encoding, so a file carries more than one
//   and their suffixed names have to differ. Its members are chosen to
//   sit in distinct wire families on BOTH backends: BOOL against FLOAT64
//   is boolean-against-float on AGE and bool-against-float64 on neo4j.
//   ANY<DATE | STRING> would divide them instead — admitted on neo4j,
//   where the driver hands back a dbtype.Date, and refused on AGE, where
//   a DATE is ISO text no probe can tell from a STRING — and that
//   divergence is measured where it can name the backend, in
//   TestAContingentRefusalNamesItsBackend.
//
// LIST<ANY<…>> is deliberately absent. neo4j's StorableProperty says
// nothing about a union-valued list today, so it is admitted by silence
// rather than by a measurement, and gqlc-npus owes the server's answer on
// heterogeneous arrays before any fixture pins a golden for it.
//
// The three reads are the three paths into the emission. The whole-entity
// :one goes through the models struct; the columns :many projects each
// union alone, which is the column position and not the same code as the
// property read; the third binds one as a PARAMETER, which is the encode
// direction and the only position where bind-time member validation can
// refuse at run time.

// name: RowWhole :one
MATCH (r:Row) RETURN r

// name: RowColumns :many
MATCH (r:Row) RETURN r.pick AS pick, r.also AS also, r.flag AS flag

// name: RowsByPick :many
MATCH (r:Row) WHERE r.pick = $pick RETURN r.id AS id
