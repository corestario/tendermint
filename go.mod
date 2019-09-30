module github.com/dgamingfoundation/tendermint

go 1.12

require (
	github.com/VividCortex/gohistogram v1.0.0 // indirect
	github.com/btcsuite/btcd v0.0.0-20190115013929-ed77733ec07d
	github.com/btcsuite/btcutil v0.0.0-20180706230648-ab6388e0c60a
	github.com/dgamingfoundation/dkglib v1.0.0
	github.com/fortytw2/leaktest v1.3.0
	github.com/go-kit/kit v0.8.0
	github.com/go-logfmt/logfmt v0.4.0
	github.com/gogo/protobuf v1.2.1
	github.com/golang/protobuf v1.3.2
	github.com/gorilla/websocket v1.4.0
	github.com/json-iterator/go v1.1.7
	github.com/libp2p/go-buffer-pool v0.0.1
	github.com/magiconair/properties v1.8.0
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v0.9.3
	github.com/rcrowley/go-metrics v0.0.0-20180503174638-e2704e165165
	github.com/rs/cors v1.6.0
	github.com/spf13/cobra v0.0.5
	github.com/spf13/viper v1.4.0
	github.com/stretchr/testify v1.3.0
	github.com/tendermint/go-amino v0.15.0
	github.com/tendermint/tendermint v0.32.3
	github.com/tendermint/tm-db v0.1.1
	go.dedis.ch/kyber/v3 v3.0.4
	golang.org/x/crypto v0.0.0-20190313024323-a1f597ede03a
	golang.org/x/net v0.0.0-20190628185345-da137c7871d7
	google.golang.org/grpc v1.22.0
)

replace github.com/tendermint/tendermint => github.com/dgamingfoundation/tendermint v0.27.4-0.20190927130609-381348170688

replace github.com/dgamingfoundation/dkglib => github.com/dgamingfoundation/dkglib v1.0.0
