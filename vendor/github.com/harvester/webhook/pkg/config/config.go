package config

// Options for the webhook server
type Options struct {
	Namespace       string
	Threadiness     int
	HTTPSListenPort int
	BindHost        string

	ControllerUsername        string
	GarbageCollectionUsername string
}
