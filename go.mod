module github.com/tendermint/tendermint

go 1.12

require (
	github.com/Workiva/go-datastructures v1.0.50
	github.com/btcsuite/btcd v0.0.0-20190115013929-ed77733ec07d
	github.com/btcsuite/btcutil v0.0.0-20180706230648-ab6388e0c60a
	github.com/corestario/dkglib v1.0.4
	github.com/fortytw2/leaktest v1.3.0
	github.com/go-kit/kit v0.9.0
	github.com/go-logfmt/logfmt v0.4.0
	github.com/gogo/protobuf v1.3.1
	github.com/golang/protobuf v1.3.2
	github.com/gorilla/websocket v1.4.1
	github.com/json-iterator/go v1.1.9
	github.com/libp2p/go-buffer-pool v0.0.2
	github.com/magiconair/properties v1.8.1
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v0.9.3
	github.com/rcrowley/go-metrics v0.0.0-20180503174638-e2704e165165
	github.com/rs/cors v1.7.0
	github.com/sirupsen/logrus v1.2.0
	github.com/snikch/goodman v0.0.0-20171125024755-10e37e294daa
	github.com/spf13/cobra v0.0.5
	github.com/spf13/viper v1.6.1
	github.com/stretchr/testify v1.4.0
	github.com/tendermint/go-amino v0.15.1
	github.com/tendermint/tm-db v0.3.0
	go.dedis.ch/kyber/v3 v3.0.9
	golang.org/x/crypto v0.0.0-20190701094942-4def268fd1a4
	golang.org/x/net v0.0.0-20190628185345-da137c7871d7
	google.golang.org/grpc v1.25.1
)

replace (
	github.com/cosmos/cosmos-sdk => github.com/corestario/cosmos-sdk v0.3.0
	github.com/tendermint/tendermint => ./
	go.dedis.ch/kyber/v3 => github.com/corestario/kyber/v3 v3.0.0-20200218082721-8ed10c357c05
)
