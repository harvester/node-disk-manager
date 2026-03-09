module github.com/harvester/node-disk-manager

go 1.25.7

replace (
	github.com/openshift/api => github.com/openshift/api v0.0.0-20191219222812-2987a591a72c
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	github.com/rancher/rancher/pkg/apis => github.com/rancher/rancher/pkg/apis v0.0.0-20240919204204-3da2ae0cabd1
	gopkg.in/yaml.v3 => gopkg.in/yaml.v3 v3.0.1
	k8s.io/api => k8s.io/api v0.35.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.35.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.35.0
	k8s.io/apiserver => k8s.io/apiserver v0.35.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.35.0
	k8s.io/client-go => k8s.io/client-go v0.35.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.35.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.35.0
	k8s.io/code-generator => k8s.io/code-generator v0.35.0
	k8s.io/component-base => k8s.io/component-base v0.35.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.35.0
	k8s.io/controller-manager => k8s.io/controller-manager v0.35.0
	k8s.io/cri-api => k8s.io/cri-api v0.35.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.35.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.35.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.35.0
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20260304202019-5b3e3fdb0acf
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.35.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.35.0
	k8s.io/kubectl => k8s.io/kubectl v0.35.0
	k8s.io/kubelet => k8s.io/kubelet v0.35.0
	k8s.io/kubernetes => k8s.io/kubernetes v1.35.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.35.0
	k8s.io/metrics => k8s.io/metrics v0.35.0
	k8s.io/mount-utils => k8s.io/mount-utils v0.35.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.35.0
)

require (
	github.com/ehazlett/simplelog v0.0.0-20200226020431-d374894e92a4
	github.com/harvester/go-common v0.0.0-20260108124725-70d352e21314
	github.com/harvester/harvester v1.5.1
	github.com/harvester/webhook v0.1.5
	github.com/jaypipes/ghw v0.8.1-0.20210701154532-dd036bd38c40
	github.com/kevinburke/ssh_config v1.2.0
	github.com/longhorn/longhorn-manager v1.11.0
	github.com/melbahja/goph v1.3.0
	github.com/pilebones/go-udev v0.0.0-20210126000448-a3c2a7a4afb7
	github.com/pkg/errors v0.9.1
	github.com/rancher/lasso v0.2.6
	github.com/rancher/wrangler/v3 v3.4.0
	github.com/sirupsen/logrus v1.9.4
	github.com/stretchr/testify v1.11.1
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/crypto v0.47.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.35.0
	k8s.io/apimachinery v0.35.0
	k8s.io/client-go v12.0.0+incompatible
)

require (
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cpuguy83/go-md2man/v2 v2.0.7 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/evanphx/json-patch v5.9.11+incompatible // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/openshift/custom-resource-status v1.1.2 // indirect
	github.com/pkg/sftp v1.13.4 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.17.0 // indirect
	github.com/rancher/dynamiclistener v0.7.3 // indirect
	github.com/rancher/wrangler v1.1.2 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.32.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/oauth2 v0.33.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	golang.org/x/term v0.39.0 // indirect
	golang.org/x/text v0.33.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	golang.org/x/tools v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	howett.net/plist v1.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.35.0 // indirect
	k8s.io/code-generator v0.35.0 // indirect
	k8s.io/gengo v0.0.0-20250130153323-76c5745d3511 // indirect
	k8s.io/gengo/v2 v2.0.0-20250922181213-ec3ebc5fd46b // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-aggregator v0.35.0 // indirect
	k8s.io/kube-openapi v0.31.5 // indirect
	k8s.io/utils v0.0.0-20260108192941-914a6e750570 // indirect
	kubevirt.io/api v1.4.0 // indirect
	kubevirt.io/containerized-data-importer-api v1.61.0 // indirect
	kubevirt.io/controller-lifecycle-operator-sdk/api v0.0.0-20220329064328-f3cc58c6ed90 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.1 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
