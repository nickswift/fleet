package unit

type SystemdUnit interface {
	Name() string
	State() (string, []string, error)
	Payload() (string, error)
}