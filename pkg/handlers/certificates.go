package handlers

import (
	"crypto/tls"
	"path/filepath"

	"golang.org/x/crypto/acme/autocert"
)

func (s *server) GetSelfSignedOrLetsEncryptCert(certManager *autocert.Manager) func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		dirCache, ok := certManager.Cache.(autocert.DirCache)
		if !ok {
			dirCache = "certs"
		}

		keyFile := filepath.Join(string(dirCache), hello.ServerName+"-key.pem")
		crtFile := filepath.Join(string(dirCache), hello.ServerName+".pem")
		certificate, err := tls.LoadX509KeyPair(crtFile, keyFile)
		if err != nil {
			s.log.Printf("%s\nFalling back to Letsencrypt\n", err)
			return certManager.GetCertificate(hello)
		}
		s.log.Println("Loaded selfsigned certificate.")
		return &certificate, err
	}
}
