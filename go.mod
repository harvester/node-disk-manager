module github.com/harvester/node-disk-manager

go 1.21

replace (
	gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.0-20220521103104-8f96da9f5d5e
	k8s.io/api => k8s.io/api v0.24.13
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.24.13
	k8s.io/apimachinery => k8s.io/apimachinery v0.24.13
	k8s.io/apiserver => k8s.io/apiserver v0.24.13
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.24.13
	k8s.io/client-go => k8s.io/client-go v0.24.13
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.24.13
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.24.13
	k8s.io/code-generator => k8s.io/code-generator v0.24.13
	k8s.io/component-base => k8s.io/component-base v0.24.13
	k8s.io/component-helpers => k8s.io/component-helpers v0.24.13
	k8s.io/controller-manager => k8s.io/controller-manager v0.24.13
	k8s.io/cri-api => k8s.io/cri-api v0.24.13
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.24.13
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.24.13
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.24.13
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.24.13
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.24.13
	k8s.io/kubectl => k8s.io/kubectl v0.24.13
	k8s.io/kubelet => k8s.io/kubelet v0.24.13
	k8s.io/kubernetes => k8s.io/kubernetes v1.24.13
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.24.13
	k8s.io/metrics => k8s.io/metrics v0.24.13
	k8s.io/mount-utils => k8s.io/mount-utils v0.24.13
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.24.13
)

require (
	github.com/ehazlett/simplelog v0.0.0-20200226020431-d374894e92a4
	github.com/harvester/go-common v0.0.0-20230718010724-11313421a8f5
	github.com/jaypipes/ghw v0.8.1-0.20210701154532-dd036bd38c40
	github.com/kevinburke/ssh_config v1.2.0
	github.com/longhorn/go-iscsi-helper v0.0.0-20231113050545-9df1e6b605c7
	github.com/longhorn/longhorn-manager v1.5.3
	github.com/melbahja/goph v1.4.0
	github.com/pilebones/go-udev v0.0.0-20210126000448-a3c2a7a4afb7
	github.com/rancher/lasso v0.0.0-20221227210133-6ea88ca2fbcc
	github.com/rancher/wrangler v1.1.1
	github.com/sirupsen/logrus v1.9.2
	github.com/stretchr/testify v1.8.2
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/crypto v0.21.0
	k8s.io/api v0.27.1
	k8s.io/apimachinery v0.27.1
	k8s.io/client-go v0.27.1
	k8s.io/utils v0.0.0-20230406110748-d93618cff8a2
)

require (
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/c9s/goprocinfo v0.0.0-20210130143923-c95fcf8c64a8 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.9.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/felixge/httpsnoop v1.0.3 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.1 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/godbus/dbus/v5 v5.0.4 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/handlers v1.5.1 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/jinzhu/copier v0.3.5 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.15.0 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	golang.org/x/mod v0.13.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/oauth2 v0.13.0 // indirect
	golang.org/x/sync v0.4.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/term v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	golang.org/x/tools v0.14.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	howett.net/plist v0.0.0-20181124034731-591f970eefbb // indirect
	k8s.io/code-generator v0.25.4 // indirect
	k8s.io/gengo v0.0.0-20211129171323-c02415ce4185 // indirect
	k8s.io/klog/v2 v2.100.1 // indirect
	k8s.io/kube-openapi v0.0.0-20230308215209-15aac26d736a // indirect
	sigs.k8s.io/controller-runtime v0.10.1 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
