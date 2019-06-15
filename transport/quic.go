package transport

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"time"

	"github.com/golang/glog"
	quic "github.com/lucas-clemente/quic-go"
	"github.com/pkg/errors"
	"github.com/wweir/sower/transport/parser"
	"github.com/wweir/sower/util"
)

type quicTran struct {
	clientConf *quic.Config
	sess       quic.Session

	serverConf *quic.Config
}

func NewQUIC() Transport {
	return &quicTran{

		clientConf: &quic.Config{
			HandshakeTimeout: time.Second,
			KeepAlive:        true,
			IdleTimeout:      time.Minute,
		},
		serverConf: &quic.Config{
			MaxIncomingStreams: 1024,
		},
	}
}

func (c *quicTran) Dial(addr, targetAddr string) (net.Conn, error) {
	if c.sess == nil {
		if sess, err := quic.DialAddr(addr, &tls.Config{InsecureSkipVerify: true}, c.clientConf); err != nil {
			return nil, errors.Wrap(err, "session")
		} else {
			go func() {
				<-sess.Context().Done()
				sess.Close()
				c.sess = nil
			}()
			c.sess = sess
		}
	}

	var stream quic.Stream
	if err := util.WithTimeout(func() (err error) {
		if stream, err = c.sess.OpenStream(); err != nil {
			c.sess = nil
		}
		return
	}, time.Second); err != nil {
		return nil, errors.Wrap(err, "stream")
	}

	return parser.WithTarget(&streamConn{
		Stream: stream,
		sess:   c.sess,
	}, targetAddr)
}

func (s *quicTran) Listen(port string) (<-chan *TargetConn, error) {
	ln, err := quic.ListenAddr(port, mockTLSPem(), s.serverConf)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	connCh := make(chan *TargetConn)
	go func() {
		for {
			sess, err := ln.Accept()
			if err != nil {
				glog.Fatalln(err)
			}
			go accept(sess, connCh)
		}
	}()
	return connCh, nil
}

func accept(sess quic.Session, connCh chan<- *TargetConn) {
	glog.V(1).Infoln("new session from ", sess.RemoteAddr())
	defer sess.Close()

	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			glog.Errorln(err)
			return
		}

		c, addr, err := parser.ParseAddr(&streamConn{stream, sess})
		if err != nil {
			glog.Errorln("parse addr:", err)
		}
		connCh <- &TargetConn{c, addr}
	}
}

// streamConn mock quic stream as a net.Conn
type streamConn struct {
	quic.Stream
	sess quic.Session
}

func (s *streamConn) LocalAddr() net.Addr {
	return s.sess.LocalAddr()
}

func (s *streamConn) RemoteAddr() net.Addr {
	return s.sess.RemoteAddr()
}

func mockTLSPem() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		glog.Fatalln(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		glog.Fatalln(err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		glog.Fatalln(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}
