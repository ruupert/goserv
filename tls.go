// Code generated by go generate; DO NOT EDIT.
// Generated at 2024-07-30 08:22:42.532776626 +0300 EEST m=+0.077746845
// using data from
// https://statics.tls.security.mozilla.org/server-side-tls-conf.json
package main

import (
	"crypto/tls"
)

var TLSConfig = &tls.Config{
	MinVersion:               tls.VersionTLS12,
	CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
	PreferServerCipherSuites: true,
	CipherSuites: []uint16{
		tls.TLS_AES_128_GCM_SHA256,
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,
	},
}
