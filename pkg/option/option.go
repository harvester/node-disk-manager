package option

type Option struct {
	KubeConfig  string
	Namespace   string
	NodeName    string
	Threadiness int

	Debug               bool
	Trace               bool
	LogFormat           string
	ProfilerAddress     string
	VendorFilter        string
	PathFilter          string
	LabelFilter         string
	AutoProvisionFilter string
	RescanInterval      int64
}
