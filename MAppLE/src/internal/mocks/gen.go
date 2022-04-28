package mocks

//go:generate sh -c "mockgen -source=../handshake/mint_utils.go -package mockhandshake -destination handshake/mint_tls.go"
//go:generate sh -c "./mockgen_internal.sh mocks stream_flow_controller.go github.com/lucas-clemente/quic-go/internal/flowcontrol StreamFlowController"
//go:generate sh -c "./mockgen_internal.sh mocks connection_flow_controller.go github.com/lucas-clemente/quic-go/internal/flowcontrol ConnectionFlowController"
//go:generate sh -c "./mockgen_internal.sh mockcrypto crypto/aead.go github.com/lucas-clemente/quic-go/internal/crypto AEAD"
//go:generate sh -c "./mockgen_stream.sh mocks stream.go github.com/lucas-clemente/quic-go StreamI"
//go:generate sh -c "./mockgen_fec.sh mocks fec_scheduler.go github.com/lucas-clemente/quic-go/fec FECScheduler"
//go:generate sh -c "./mockgen_fec.sh mocks mock_net_watcher.go github.com/lucas-clemente/quic-go NetWatcherI"
//go:generate sh -c "goimports -w ."
