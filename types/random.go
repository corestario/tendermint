package types

// Random share contains validator's data about the shared random value.
type RandomShare struct {
	*Vote
	Data []byte
}

func (m *RandomShare) SignBytes(chainID string) []byte {
	bz, err := cdc.MarshalBinaryLengthPrefixed(CanonicalizeRandomShare(chainID, m))
	if err != nil {
		panic(err)
	}
	return bz
}
