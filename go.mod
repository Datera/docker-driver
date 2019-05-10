module github.com/Datera/docker-driver

go 1.12

replace (
	github.com/Sirupsen/logrus v1.0.5 => github.com/sirupsen/logrus v1.0.5
	github.com/Sirupsen/logrus v1.3.0 => github.com/Sirupsen/logrus v1.0.6
	github.com/Sirupsen/logrus v1.4.0 => github.com/sirupsen/logrus v1.0.6
)

require (
	github.com/Datera/datera-csi v1.0.7
	github.com/Datera/go-sdk v1.1.6
	github.com/Datera/go-udc v1.1.1
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-plugins-helpers v0.0.0-20181025120712-1e6269c305b8
	github.com/facebookgo/stack v0.0.0-20160209184415-751773369052 // indirect
	github.com/google/pprof v0.0.0-20190309163659-77426154d546
	github.com/google/uuid v1.1.1
	github.com/gurpartap/logrus-stack v0.0.0-20170710170904-89c00d8a28f4
	github.com/ianlancetaylor/demangle v0.0.0-20181102032728-5e5cf60278f6 // indirect
	github.com/kubernetes-csi/csi-lib-iscsi v0.0.0-20190415173011-c545557492f4
	github.com/satori/go.uuid v1.2.0
	github.com/sirupsen/logrus v1.4.1
	golang.org/x/arch v0.0.0-20190312162104-788fe5ffcd8c
	golang.org/x/crypto v0.0.0-20190313024323-a1f597ede03a
	golang.org/x/net v0.0.0-20190503192946-f4e77d36d62c // indirect
	golang.org/x/tools v0.0.0-20190318200714-bb1270c20edf
)
