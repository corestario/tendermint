package consensus

func NewDealerConstructor(indexToConstructor map[int]DKGDealerConstructor) func(i int) DKGDealerConstructor {
	return func(i int) DKGDealerConstructor {
		if constructor, ok := indexToConstructor[i]; ok {
			return constructor
		}
		return NewDKGDealer
	}
}
