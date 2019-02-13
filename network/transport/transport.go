package transport

// Layer represents a transport protocol layer.
type Layer interface {
	Listen(port int) (interface{}, error)
	Dial(address string) (interface{}, error)
}
