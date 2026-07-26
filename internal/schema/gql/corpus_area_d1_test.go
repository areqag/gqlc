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
	},
	{
		file:    "18.9-value-type/scalar_string_length_hex.gql",
		outcome: resolves,
		feature: "mandatory",
	},
	{
		file:    "18.9-value-type/scalar_string_length_octal.gql",
		outcome: resolves,
		feature: "mandatory",
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
	},
	{
		file:    "18.9-value-type/scalar_varchar_max_length.gql",
		outcome: resolves,
		feature: "mandatory",
		bead:    "gqlc-5md",
		reason:  "the maxLength is discarded, so VARCHAR(10) resolves to a PropertyType byte-identical to bare VARCHAR (both map to TypeString with no width); the two are indistinguishable downstream and codegen cannot emit a width",
	},

	{
		file:     "18.9-value-type/scalar_binary.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "BINARY is a byte-string type; gqlc's PropertyType has no byte-string family, so every byteStringType spelling is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_bytes.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "BYTES is a byte-string type; gqlc's PropertyType has no byte-string family, so every byteStringType spelling is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_varbinary.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "VARBINARY is a byte-string type; gqlc's PropertyType has no byte-string family, so every byteStringType spelling is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_duration_day_second.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "DURATION is a temporalDurationType; gqlc's PropertyType has no duration family, so DAY TO SECOND is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_duration_year_month.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "DURATION is a temporalDurationType; gqlc's PropertyType has no duration family, so YEAR TO MONTH is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_local_time.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "LOCAL TIME is a time-of-day type; graph.PropertyType has TypeDate and TypeTimestamp and no time-of-day type at all, so every timeType and localtimeType spelling is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_signed_verbose_integer.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "the SIGNED keyword prefix on a verboseBinaryExactNumericType canonicalises to \"SIGNED INTEGER\", which is not in typeSpellings; typeSpellings has no explicit signedness entry so SIGNED verbose numerics are rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_time_with_time_zone.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "TIME WITH TIME ZONE is a time-of-day type; graph.PropertyType has no time-of-day type at all, so every timeType spelling is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_time_without_time_zone.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "TIME WITHOUT TIME ZONE is a time-of-day type; graph.PropertyType has no time-of-day type at all, so every localtimeType spelling is rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_unsigned_verbose_integer.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "the UNSIGNED keyword prefix on a verboseBinaryExactNumericType canonicalises to \"UNSIGNED INTEGER\", which is not in typeSpellings; typeSpellings has no explicit signedness entry so UNSIGNED verbose numerics are rejected until one is added",
	},
	{
		file:     "18.9-value-type/scalar_zoned_time.gql",
		outcome:  unsupported,
		sentinel: ErrUnsupportedType,
		feature:  "mandatory",
		bead:     "gqlc-h9n.4",
		reason:   "ZONED TIME is a time-of-day type; graph.PropertyType has no time-of-day type at all, so every timeType spelling is rejected until one is added",
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
	},
	{
		file:     "18.9-value-type/scalar_decimal_precision_scale.gql",
		bead:     "gqlc-5md",
		why:      "DECIMAL(10,2) resolves to the same PropertyType as bare DECIMAL, because PropertyType has no length field; the discarded precision and scale are unrecoverable downstream",
		spelling: "DECIMAL(10,2)",
	},
	{
		file:     "18.9-value-type/scalar_string_max_length.gql",
		bead:     "gqlc-5md",
		why:      "STRING(5) resolves to the same PropertyType as bare STRING, because PropertyType has no length field; the discarded maxLength is unrecoverable downstream",
		spelling: "STRING(5)",
	},
	{
		file: "18.9-value-type/scalar_string_min_max_length.gql",
		bead: "gqlc-5md",
		why:  "STRING(2, 5) resolves to the same PropertyType as bare STRING, because PropertyType has no length field; both the discarded minLength and maxLength are unrecoverable downstream",
		// The file spells this with a space after the comma and the other length
		// cases do not; ISO allows either, and the point of pinning the exact
		// spelling is lost if it is normalised to match its neighbours here.
		spelling: "STRING(2, 5)",
	},
	{
		file:     "18.9-value-type/scalar_varchar_max_length.gql",
		bead:     "gqlc-5md",
		why:      "VARCHAR(10) resolves to the same PropertyType as bare VARCHAR (and bare STRING), because PropertyType has no length field; the discarded maxLength is unrecoverable downstream",
		spelling: "VARCHAR(10)",
	},
}
