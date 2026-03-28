package network

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"testing"
	"time"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
testQUICTLSConfigs generates a throwaway self-signed ECDSA cert and
returns a server TLS config (with the cert) and a matching client config
(skip-verify, same ALPN). Only for tests.
*/
func testQUICTLSConfigs() (*tls.Config, *tls.Config) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	if err != nil {
		panic(err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	certDER, err := x509.CreateCertificate(
		rand.Reader, template, template, &key.PublicKey, key,
	)

	if err != nil {
		panic(err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	serverConf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"six-test"},
		MinVersion:   tls.VersionTLS13,
	}

	clientConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"six-test"},
		MinVersion:         tls.VersionTLS13,
	}

	return serverConf, clientConf
}

/*
setupQUICPair creates a connected QUIC server/client pair for testing.
The transport now primes stream readiness internally, so callers can use
the stream directly without protocol-specific trigger writes.
*/
func setupQUICPair(t testing.TB) (server *QUIC, client *QUIC) {
	t.Helper()

	serverTLS, clientTLS := testQUICTLSConfigs()

	server = NewQUIC(QUICWithListen("127.0.0.1:0", serverTLS))

	if server.err != nil {
		t.Fatal(server.err)
	}

	addr := server.endpoint.LocalAddr().String()

	accepted := make(chan error, 1)
	go func() {
		accepted <- server.Accept()
	}()

	client = NewQUIC(QUICWithDial(addr, clientTLS))

	if client.err != nil {
		t.Fatal(client.err)
	}

	if err := <-accepted; err != nil {
		t.Fatal(err)
	}

	return server, client
}

func TestQUIC(t *testing.T) {
	gc.Convey("Given a QUIC transport with no stream", t, func() {
		q := NewQUIC()

		gc.Convey("It should return ErrQUICNoStream on Read", func() {
			buf := make([]byte, 1024)
			_, err := q.Read(buf)
			gc.So(errors.Is(err, ErrQUICNoStream), gc.ShouldBeTrue)
		})

		gc.Convey("It should return ErrQUICNoStream on Write", func() {
			_, err := q.Write([]byte("data"))
			gc.So(errors.Is(err, ErrQUICNoStream), gc.ShouldBeTrue)
		})

		gc.Convey("It should return ErrQUICNotListening on Accept", func() {
			gc.So(errors.Is(q.Accept(), ErrQUICNotListening), gc.ShouldBeTrue)
		})

		gc.Convey("It should close without error", func() {
			gc.So(q.Close(), gc.ShouldBeNil)
		})
	})
}

func TestQUICRoundtrip(t *testing.T) {
	gc.Convey("Given a connected QUIC server and client", t, func() {
		server, client := setupQUICPair(t)

		gc.Convey("It should transfer data bidirectionally", func() {
			payload := make([]byte, 1024)
			for idx := range payload {
				payload[idx] = byte(idx % 256)
			}

			n, err := client.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1024)
			n, err = io.ReadFull(server, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf, gc.ShouldResemble, payload)

			reply := make([]byte, 1024)
			reply[0] = 0xDE
			reply[1023] = 0xAD

			n, err = server.Write(reply)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			n, err = io.ReadFull(client, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0xDE)
			gc.So(buf[1023], gc.ShouldEqual, 0xAD)
		})

		gc.Reset(func() {
			client.Close()
			server.Close()
		})
	})
}

func TestQUICWithStream(t *testing.T) {
	gc.Convey("Given a QUIC transport wrapping an externally-managed stream", t, func() {
		server, client := setupQUICPair(t)

		wrapped := NewQUIC(QUICWithStream(server.stream))

		gc.Convey("It should read through the wrapped stream with nil conn and endpoint", func() {
			gc.So(wrapped.conn, gc.ShouldBeNil)
			gc.So(wrapped.endpoint, gc.ShouldBeNil)
			gc.So(wrapped.stream, gc.ShouldNotBeNil)

			payload := make([]byte, 1024)
			payload[0] = 0xBE

			n, err := client.Write(payload)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)

			buf := make([]byte, 1024)
			n, err = io.ReadFull(wrapped, buf)
			gc.So(err, gc.ShouldBeNil)
			gc.So(n, gc.ShouldEqual, 1024)
			gc.So(buf[0], gc.ShouldEqual, 0xBE)
		})

		gc.Reset(func() {
			wrapped.Close()
			client.Close()
			server.Close()
		})
	})
}

func TestQUICCloseWithResources(t *testing.T) {
	gc.Convey("Given a fully-connected QUIC transport", t, func() {
		server, client := setupQUICPair(t)

		gc.Convey("Close should tear down all three layers and cancel the context", func() {
			gc.So(client.stream, gc.ShouldNotBeNil)
			gc.So(client.conn, gc.ShouldNotBeNil)
			gc.So(client.endpoint, gc.ShouldNotBeNil)
			gc.So(client.ctx.Err(), gc.ShouldBeNil)

			client.Close()
			server.Close()

			gc.So(client.ctx.Err(), gc.ShouldNotBeNil)
		})
	})
}

func TestQUICWithContext(t *testing.T) {
	gc.Convey("Given a QUIC transport with a custom context", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		q := NewQUIC(QUICWithContext(ctx))

		gc.Convey("It should propagate cancellation from the parent context", func() {
			gc.So(q.ctx.Err(), gc.ShouldBeNil)
			cancel()
			gc.So(q.ctx.Err(), gc.ShouldNotBeNil)
		})

		gc.Reset(func() {
			cancel()
			q.Close()
		})
	})
}

func TestQUICErrorString(t *testing.T) {
	gc.Convey("Given QUICError constants", t, func() {
		gc.Convey("ErrQUICNoStream should satisfy the error interface", func() {
			var err error = ErrQUICNoStream
			gc.So(err.Error(), gc.ShouldEqual, "quic: no active stream")
		})

		gc.Convey("ErrQUICNotListening should satisfy the error interface", func() {
			var err error = ErrQUICNotListening
			gc.So(err.Error(), gc.ShouldEqual, "quic: no endpoint listening")
		})
	})
}

func TestQUICImplementsRWC(t *testing.T) {
	gc.Convey("Given a QUIC transport", t, func() {
		q := NewQUIC()

		gc.Convey("It should satisfy io.ReadWriteCloser", func() {
			var _ io.ReadWriteCloser = q
			gc.So(q, gc.ShouldNotBeNil)
		})
	})
}

func BenchmarkQUICRoundtrip(b *testing.B) {
	server, client := setupQUICPair(b)

	payload := make([]byte, 1024)
	sink := make([]byte, 1024)

	go func() {
		for {
			_, err := server.Read(sink)

			if err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()
	b.SetBytes(1024)

	for b.Loop() {
		client.Write(payload)
	}

	b.StopTimer()
	client.Close()
	server.Close()
}
