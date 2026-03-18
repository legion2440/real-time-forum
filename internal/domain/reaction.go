package domain

type ReactionChange struct {
	PreviousValue int
	CurrentValue  int
}

func (c ReactionChange) Changed() bool {
	return c.PreviousValue != c.CurrentValue
}
