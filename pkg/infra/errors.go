package infra

type EmptyRoutError struct {
}

func (EmptyRoutError) Error() string {
	return "empty route"
}
