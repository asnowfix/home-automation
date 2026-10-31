package roomba

import (
	"crypto/tls"
	"fmt"
)

// MQTTPort is where the robot's local MQTT-over-TLS broker listens.
const MQTTPort = 8883

// TLSConfig returns the client TLS configuration the robot accepts.
//
// The robot only negotiates TLS 1.2 with TLS_RSA_WITH_AES_128_CBC_SHA256 and needs
// legacy renegotiation, which Go's defaults exclude. Its certificate is issued by
// iRobot's private intermediate CA, whose root we do not have, so the chain cannot
// be verified. Instead, when blid is not empty, the leaf certificate's subject CN
// must be "iRobot-<blid>": that ties the connection to the robot we meant to reach,
// even after its DHCP address moves to another device.
func TLSConfig(blid string) *tls.Config {
	cfg := &tls.Config{
		InsecureSkipVerify: true, // chain verified by hand below, see doc comment
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS12,
		CipherSuites:       []uint16{tls.TLS_RSA_WITH_AES_128_CBC_SHA256},
		Renegotiation:      tls.RenegotiateFreelyAsClient,
	}
	if blid != "" {
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			return verifyRobotCN(cs, blid)
		}
	}
	return cfg
}

func verifyRobotCN(cs tls.ConnectionState, blid string) error {
	if len(cs.PeerCertificates) == 0 {
		return fmt.Errorf("roomba: robot presented no certificate")
	}
	want := "iRobot-" + blid
	if got := cs.PeerCertificates[0].Subject.CommonName; got != want {
		return fmt.Errorf("roomba: certificate CN %q is not the expected robot %q", got, want)
	}
	return nil
}
