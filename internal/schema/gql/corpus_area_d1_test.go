package gql

// corpusAreaD1 holds the corpus entries for predefined scalar value types —
// booleans, character and byte strings, exact and approximate numerics, temporal
// types and their qualifiers. It shares 18.9-value-type/ with area D2, which takes
// the constructed and reference types; the two are disjoint by file, not by
// directory. One area variable per author so that two authors never edit the same
// Go file; corpusAreas fixes the directories these entries may live in.
var corpusAreaD1 = []corpusEntry{
	{
		file:    "18.9-value-type/scalar_boolean.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_char_fixed_length.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the fixedLength is discarded, so CHAR(4) resolves to a PropertyType byte-identical to bare CHAR (both map to TypeString with no width); the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_character_string.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_dec_alias.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the precision is discarded, so DEC(8) resolves to a PropertyType byte-identical to bare DEC (both map to graph.TypeDecimal, which carries no precision). DEC is DECIMAL's alias and maps to the same constant, so this is scalar_decimal_precision_scale.gql's discard reached through the shorter spelling",
	},
	{
		file:    "18.9-value-type/scalar_date.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_datetime_zoned.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_datetime_with_time_zone.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_decimal_precision_scale.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the precision and scale are discarded, so DECIMAL(10,2) resolves to a PropertyType byte-identical to bare DECIMAL; the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_float.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-h9n.31",
		reason:  "FLOAT's parenthetical is never folded, so FLOAT(10) and FLOAT(10, 2) both resolve to TypeFloat, byte-identical to bare FLOAT; ADR 0017 accepts the loss, and gqlc-h9n.28 — whether that precision counts bits or digits — is why the owning bead is provisional",
	},
	{
		file:    "18.9-value-type/scalar_local_datetime.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_signed_integer.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-h9n.31",
		reason:  "10 is not a width PropertyType enumerates, so INT(10) and INTEGER(10) fall through the truncating fallback to TypeInt — a 64-bit machine int, byte-identical to bare INT; ADR 0017 accepts the loss and this is where it is recorded",
	},
	{
		file:    "18.9-value-type/scalar_timestamp.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_timestamp_without_time_zone.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_string_length_binary.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the length is discarded whatever base spells it, so STRING(0b101) resolves to a PropertyType byte-identical to bare STRING (both graph.TypeString with no width). The binary literal is what this file is for; that the model cannot tell 5 from unbounded is the same gap scalar_string_max_length.gql records",
	},
	{
		file:    "18.9-value-type/scalar_string_length_hex.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the length is discarded whatever base spells it, so STRING(0xFF) resolves to a PropertyType byte-identical to bare STRING (both graph.TypeString with no width). The hexadecimal literal is what this file is for, and 255 is the largest of the three bases here — the model keeps none of it",
	},
	{
		file:    "18.9-value-type/scalar_string_length_octal.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the length is discarded whatever base spells it, so STRING(0o17) resolves to a PropertyType byte-identical to bare STRING (both graph.TypeString with no width). The octal literal is what this file is for; the three base files differ in the lexer and agree in the model, which is the discard stated three ways",
	},
	{
		file:    "18.9-value-type/scalar_string_max_length.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the maxLength is discarded, so STRING(5) resolves to a PropertyType byte-identical to bare STRING; the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_string_min_max_length.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "both minLength and maxLength are discarded, so STRING(2,5) resolves to a PropertyType byte-identical to bare STRING; the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_unsigned_integer.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-h9n.31",
		reason:  "10 is not a width PropertyType enumerates, so UINT(10) falls through the truncating fallback to TypeUint — a 64-bit machine uint, byte-identical to bare UINT; ADR 0017 accepts the loss and this is where it is recorded",
	},
	{
		file:    "18.9-value-type/scalar_varchar_max_length.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the maxLength is discarded, so VARCHAR(10) resolves to a PropertyType byte-identical to bare VARCHAR (both map to TypeString with no width); the two are indistinguishable downstream and codegen cannot emit a width",
	},

	{
		file:    "18.9-value-type/scalar_binary.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the fixedLength is discarded, so BINARY(4) resolves to a PropertyType byte-identical to bare BINARY; the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_bytes.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "both minLength and maxLength are discarded, so BYTES(1, 10) resolves to a PropertyType byte-identical to bare BYTES; the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_varbinary.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the maxLength is discarded, so VARBINARY(8) resolves to a PropertyType byte-identical to bare VARBINARY; the two are indistinguishable downstream and codegen cannot emit a width",
	},
	{
		file:    "18.9-value-type/scalar_duration_day_second.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-8pe",
		reason:  "the temporalDurationQualifier is discarded, so DURATION(DAY TO SECOND) resolves to a PropertyType byte-identical to DURATION(YEAR TO MONTH) (both map to TypeDuration with no qualifier); the two are indistinguishable downstream and codegen cannot preserve which fields of dbtype.Duration the value populates",
	},
	{
		file:    "18.9-value-type/scalar_duration_year_month.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-8pe",
		reason:  "the temporalDurationQualifier is discarded, so DURATION(YEAR TO MONTH) resolves to a PropertyType byte-identical to DURATION(DAY TO SECOND) (both map to TypeDuration with no qualifier); the two are indistinguishable downstream and codegen cannot preserve which fields of dbtype.Duration the value populates",
	},
	{
		file:    "18.9-value-type/scalar_local_time.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_signed_verbose_integer.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_time_with_time_zone.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_time_without_time_zone.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_unsigned_verbose_integer.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_zoned_time.gql",
		outcome: resolves,
		feature: "mandatory",
	},
}

// semanticAreaD1 holds this area's semantic cases: files above that resolve to a
// model known to be wrong.
var semanticAreaD1 = []semanticCase{
	{
		file:     "18.9-value-type/scalar_char_fixed_length.gql",
		bead:     "gqlc-5md",
		why:      "CHAR(4) resolves to the same PropertyType as bare CHAR (and bare STRING), because PropertyType has no length field; the discarded fixedLength is unrecoverable downstream",
		spelling: "CHAR(4)",
		siblings: []string{"CHAR", "STRING"},
	},
	{
		file:     "18.9-value-type/scalar_dec_alias.gql",
		bead:     "gqlc-5md",
		why:      "DEC(8) resolves to the same PropertyType as bare DEC, because PropertyType has no precision field. DEC and DECIMAL both map to graph.TypeDecimal, so this is the alias half of the collision scalar_decimal_precision_scale.gql records, and the file already spells bare DEC beside it",
		spelling: "DEC(8)",
		siblings: []string{"DEC"},
	},
	{
		file:     "18.9-value-type/scalar_decimal_precision_scale.gql",
		bead:     "gqlc-5md",
		why:      "DECIMAL(10,2) resolves to the same PropertyType as DECIMAL(8), and as bare DECIMAL, because PropertyType has no length field; the discarded precision and scale are unrecoverable downstream",
		spelling: "DECIMAL(10,2)",
		// Bare DECIMAL is the collision the why names second and cannot be the
		// sibling: decimalExactNumericType (GQL.g4:1832) puts notNull? inside the
		// parenthesised group, so `DECIMAL NOT NULL` is not GQL and substituting it
		// into this file is a syntax error. DECIMAL(8) isolates the same discard —
		// one keyword, two parentheticals, one model — without the unrelated second
		// difference a DEC alias or a nullability edit would bring with it.
		siblings: []string{"DECIMAL(8)"},
	},
	{
		file:     "18.9-value-type/scalar_string_length_binary.gql",
		bead:     "gqlc-5md",
		why:      "STRING(0b101) resolves to the same PropertyType as bare STRING, because PropertyType has no length field. The three base files exist to exercise the lexer's binary, hexadecimal and octal literals; that all three agree with bare STRING in the model is the discard restated in each base",
		spelling: "STRING(0b101)",
		siblings: []string{"STRING"},
	},
	{
		file:     "18.9-value-type/scalar_string_length_hex.gql",
		bead:     "gqlc-5md",
		why:      "STRING(0xFF) resolves to the same PropertyType as bare STRING, because PropertyType has no length field; 255 is the largest length the three base files spell and the model keeps none of it",
		spelling: "STRING(0xFF)",
		siblings: []string{"STRING"},
	},
	{
		file:     "18.9-value-type/scalar_string_length_octal.gql",
		bead:     "gqlc-5md",
		why:      "STRING(0o17) resolves to the same PropertyType as bare STRING, because PropertyType has no length field; the octal literal reaches the same discard as the binary and hexadecimal spellings",
		spelling: "STRING(0o17)",
		siblings: []string{"STRING"},
	},
	{
		file:     "18.9-value-type/scalar_string_max_length.gql",
		bead:     "gqlc-5md",
		why:      "STRING(5) resolves to the same PropertyType as bare STRING, because PropertyType has no length field; the discarded maxLength is unrecoverable downstream",
		spelling: "STRING(5)",
		siblings: []string{"STRING"},
	},
	{
		file: "18.9-value-type/scalar_string_min_max_length.gql",
		bead: "gqlc-5md",
		why:  "STRING(2, 5) resolves to the same PropertyType as bare STRING, because PropertyType has no length field; both the discarded minLength and maxLength are unrecoverable downstream",
		// The file spells this with a space after the comma and the other length
		// cases do not; ISO allows either, and the point of pinning the exact
		// spelling is lost if it is normalised to match its neighbours here.
		spelling: "STRING(2, 5)",
		siblings: []string{"STRING"},
	},
	{
		file:     "18.9-value-type/scalar_varchar_max_length.gql",
		bead:     "gqlc-5md",
		why:      "VARCHAR(10) resolves to the same PropertyType as bare VARCHAR (and bare STRING), because PropertyType has no length field; the discarded maxLength is unrecoverable downstream",
		spelling: "VARCHAR(10)",
		siblings: []string{"VARCHAR", "STRING"},
	},
	{
		file:     "18.9-value-type/scalar_binary.gql",
		bead:     "gqlc-5md",
		why:      "BINARY(4) resolves to the same PropertyType as bare BINARY, because PropertyType has no length field; the discarded fixedLength is unrecoverable downstream",
		spelling: "BINARY(4)",
		siblings: []string{"BINARY"},
	},
	{
		file: "18.9-value-type/scalar_bytes.gql",
		bead: "gqlc-5md",
		why:  "BYTES(1, 10) resolves to the same PropertyType as bare BYTES, because PropertyType has no length field; both the discarded minLength and maxLength are unrecoverable downstream",
		// Matches the file's spelling exactly, comma-space and all — the point of
		// pinning the exact spelling is lost if it is normalised to match its
		// neighbours here. Same discipline as scalar_string_min_max_length.
		spelling: "BYTES(1, 10)",
		siblings: []string{"BYTES"},
	},
	{
		file:     "18.9-value-type/scalar_varbinary.gql",
		bead:     "gqlc-5md",
		why:      "VARBINARY(8) resolves to the same PropertyType as bare VARBINARY, because PropertyType has no length field; the discarded maxLength is unrecoverable downstream",
		spelling: "VARBINARY(8)",
		siblings: []string{"VARBINARY"},
	},
	{
		file:     "18.9-value-type/scalar_duration_day_second.gql",
		bead:     "gqlc-8pe",
		why:      "DURATION(DAY TO SECOND) resolves to the same PropertyType as DURATION(YEAR TO MONTH), because PropertyType has no temporal-duration-qualifier field; the discarded qualifier selects which fields of dbtype.Duration (Days/Seconds/Nanos vs Months) a value populates at write time, and that selection is unrecoverable downstream",
		spelling: "DURATION(DAY TO SECOND)",
		siblings: []string{"DURATION(YEAR TO MONTH)"},
	},
	{
		file:     "18.9-value-type/scalar_duration_year_month.gql",
		bead:     "gqlc-8pe",
		why:      "DURATION(YEAR TO MONTH) resolves to the same PropertyType as DURATION(DAY TO SECOND), because PropertyType has no temporal-duration-qualifier field; the discarded qualifier selects which fields of dbtype.Duration (Months vs Days/Seconds/Nanos) a value populates at write time, and that selection is unrecoverable downstream",
		spelling: "DURATION(YEAR TO MONTH)",
		siblings: []string{"DURATION(DAY TO SECOND)"},
	},
	{
		file: "18.9-value-type/scalar_signed_integer.gql",
		bead: "gqlc-h9n.31",
		why:  "INT(10) resolves to the same PropertyType as bare INT, as INTEGER, and as any other off-lattice width such as INT(7), because PropertyType enumerates eight widths and 10 is not one of them; the declared ten bits are unrecoverable downstream",
		// INT(8) is deliberately absent from siblings, and it is the row's contrast
		// rather than an oversight: it folds to TypeInt8 (gqlc-h9n.16), so it does
		// not collide, and it is the reason the loss here is jagged rather than
		// uniform. ADR 0017 is the decision to accept that.
		spelling: "INT(10)",
		siblings: []string{"INT", "INTEGER", "INTEGER(10)", "INT(7)"},
	},
	{
		file:     "18.9-value-type/scalar_unsigned_integer.gql",
		bead:     "gqlc-h9n.31",
		why:      "UINT(10) resolves to the same PropertyType as bare UINT and as any other off-lattice width such as UINT(7), because PropertyType enumerates eight widths and 10 is not one of them; the declared ten bits are unrecoverable downstream",
		spelling: "UINT(10)",
		siblings: []string{"UINT", "UINT(7)"},
	},
	{
		file: "18.9-value-type/scalar_float.gql",
		bead: "gqlc-h9n.31",
		why:  "FLOAT(10) resolves to the same PropertyType as bare FLOAT and as FLOAT(10, 2), because FLOAT's parenthetical is not folded at all (ADR 0002) and PropertyType has nowhere to keep it; whether the discarded precision counts mantissa bits or decimal digits is gqlc-h9n.28, which is why this row's bead is the provisional one",
		// FLOAT(10, 2) as a sibling makes the collision class wider than the other
		// two rows': it pins that precision-with-scale is no more distinguishable
		// than precision alone, which is the shape the h9n.28 question turns on.
		spelling: "FLOAT(10)",
		siblings: []string{"FLOAT", "FLOAT(10, 2)"},
	},
}
