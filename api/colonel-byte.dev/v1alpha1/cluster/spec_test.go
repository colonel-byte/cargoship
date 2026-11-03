// Copyright 2026 colonel-byte
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cluster

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZarfClusterProfilesResolveConcurrency(t *testing.T) {
	t.Run("empty falls back", func(t *testing.T) {
		p := Profiles{}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 5, v)
	})

	t.Run("empty falls back to percentage", func(t *testing.T) {
		p := Profiles{}
		v, err := p.ResolveConcurrency(10, "25%")
		require.NoError(t, err)
		require.Equal(t, 3, v)
	})

	t.Run("fixed count is used as-is", func(t *testing.T) {
		p := Profiles{Concurrency: "1"}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 1, v)
	})

	t.Run("fixed zero means unlimited", func(t *testing.T) {
		p := Profiles{Concurrency: "0"}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 0, v)
	})

	t.Run("percentage scales and rounds up", func(t *testing.T) {
		p := Profiles{Concurrency: "25%"}
		v, err := p.ResolveConcurrency(10, "5")
		require.NoError(t, err)
		require.Equal(t, 3, v)
	})

	t.Run("percentage clamps to a minimum of 1", func(t *testing.T) {
		p := Profiles{Concurrency: "25%"}
		v, err := p.ResolveConcurrency(3, "5")
		require.NoError(t, err)
		require.Equal(t, 1, v)
	})

	t.Run("100 percent covers all hosts", func(t *testing.T) {
		p := Profiles{Concurrency: "100%"}
		v, err := p.ResolveConcurrency(7, "5")
		require.NoError(t, err)
		require.Equal(t, 7, v)
	})

	t.Run("negative fixed count is an error", func(t *testing.T) {
		p := Profiles{Concurrency: "-1"}
		_, err := p.ResolveConcurrency(10, "5")
		require.Error(t, err)
	})

	t.Run("out of range percentage is an error", func(t *testing.T) {
		p := Profiles{Concurrency: "150%"}
		_, err := p.ResolveConcurrency(10, "5")
		require.Error(t, err)
	})

	t.Run("non-numeric value is an error", func(t *testing.T) {
		p := Profiles{Concurrency: "abc"}
		_, err := p.ResolveConcurrency(10, "5")
		require.Error(t, err)
	})
}

func TestParseConcurrency(t *testing.T) {
	t.Run("empty means unlimited", func(t *testing.T) {
		v, err := ParseConcurrency("", 10)
		require.NoError(t, err)
		require.Equal(t, 0, v)
	})

	t.Run("fixed count is used as-is", func(t *testing.T) {
		v, err := ParseConcurrency("5", 10)
		require.NoError(t, err)
		require.Equal(t, 5, v)
	})

	t.Run("percentage scales and rounds up", func(t *testing.T) {
		v, err := ParseConcurrency("25%", 10)
		require.NoError(t, err)
		require.Equal(t, 3, v)
	})

	t.Run("negative fixed count is an error", func(t *testing.T) {
		_, err := ParseConcurrency("-1", 10)
		require.Error(t, err)
	})

	t.Run("non-numeric value is an error", func(t *testing.T) {
		_, err := ParseConcurrency("abc", 10)
		require.Error(t, err)
	})
}

func TestZarfClusterRegistriesValidate(t *testing.T) {
	cases := map[string]struct {
		registry Registries
		wantErr  string
	}{
		"a mirror alone is enough": {
			registry: Registries{
				Name:  "docker.io",
				Proxy: &RegistryProxy{URL: "https://mirror.example.com"},
			},
		},
		"credentials alone are enough, for a direct pull": {
			registry: Registries{
				Name:           "nexus.example.com",
				Authentication: RegistryAuth{Username: "robot", Password: "secretpassword"},
			},
		},
		"tls alone is enough": {
			registry: Registries{
				Name: "nexus.example.com",
				TLS:  &RegistryTLS{CAFile: "/etc/pki/ca.crt"},
			},
		},
		"a name on its own configures nothing": {
			registry: Registries{Name: "nexus.example.com"},
			wantErr:  `registry "nexus.example.com": needs at least one of proxy.url, auth, or tls`,
		},
		"a nameless entry has nothing to apply to": {
			registry: Registries{
				Proxy: &RegistryProxy{URL: "https://mirror.example.com"},
			},
			wantErr: "registry: name is required",
		},
		"a name written as a url matches no image": {
			registry: Registries{
				Name:  "https://docker.io",
				Proxy: &RegistryProxy{URL: "https://mirror.example.com"},
			},
			wantErr: `registry "https://docker.io": name is a registry host, not a URL -- drop the scheme`,
		},
		"a name carrying a repository path matches no image": {
			registry: Registries{
				Name:  "docker.io/library",
				Proxy: &RegistryProxy{URL: "https://mirror.example.com"},
			},
			wantErr: `registry "docker.io/library": name is a registry host, not a repository path -- drop everything after the host`,
		},
		"a name with whitespace in it matches no image": {
			registry: Registries{
				Name:  "docker.io ",
				Proxy: &RegistryProxy{URL: "https://mirror.example.com"},
			},
			wantErr: `registry "docker.io ": name contains whitespace`,
		},
		"every registry at once is a name an engine knows": {
			registry: Registries{
				Name: "*",
				TLS:  &RegistryTLS{InsecureSkipVerify: true},
			},
		},
		"a proxy url with no host redirects pulls nowhere": {
			registry: Registries{
				Name:  "docker.io",
				Proxy: &RegistryProxy{URL: "https:///v2"},
			},
			wantErr: `registry "docker.io": proxy.url "https:///v2" has no host`,
		},
		"a proxy url an engine cannot pull over": {
			registry: Registries{
				Name:  "docker.io",
				Proxy: &RegistryProxy{URL: "ftp://mirror.example.com"},
			},
			wantErr: `registry "docker.io": proxy.url "ftp://mirror.example.com" uses scheme "ftp", want http or https`,
		},
		"credentials belong in auth, not in the proxy url": {
			registry: Registries{
				Name:  "docker.io",
				Proxy: &RegistryProxy{URL: "https://robot:secretpassword@mirror.example.com"},
			},
			wantErr: `registry "docker.io": proxy.url carries credentials -- put them in auth instead`,
		},
		"a rewrite the engine could not compile": {
			registry: Registries{
				Name: "docker.io",
				Proxy: &RegistryProxy{
					URL:     "https://mirror.example.com",
					Rewrite: map[string]string{"^rancher/(.*": "mirror/$1"},
				},
			},
			wantErr: "registry \"docker.io\": proxy.rewrite pattern \"^rancher/(.*\" is not a valid regular expression: error parsing regexp: missing closing ): `^rancher/(.*`",
		},
		"a rewrite with nothing to rewrite to": {
			registry: Registries{
				Name: "docker.io",
				Proxy: &RegistryProxy{
					URL:     "https://mirror.example.com",
					Rewrite: map[string]string{"^rancher/(.*)": ""},
				},
			},
			wantErr: `registry "docker.io": proxy.rewrite pattern "^rancher/(.*)" has an empty replacement`,
		},
		"half of a basic auth pair authenticates nothing": {
			registry: Registries{
				Name:           "nexus.example.com",
				Authentication: RegistryAuth{Username: "robot"},
			},
			wantErr: `registry "nexus.example.com": auth.user is set without auth.pass`,
		},
		"a password with nobody to go with": {
			registry: Registries{
				Name:           "nexus.example.com",
				Authentication: RegistryAuth{Password: "secretpassword"},
			},
			wantErr: `registry "nexus.example.com": auth.pass is set without auth.user`,
		},
		"a proxy without a url redirects pulls nowhere": {
			registry: Registries{
				Name:  "docker.io",
				Proxy: &RegistryProxy{Rewrite: map[string]string{"^rancher/(.*)": "mirror/$1"}},
			},
			wantErr: `registry "docker.io": proxy.url is required when proxy is set`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.registry.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func TestZarfClusterRegistryTLSValidate(t *testing.T) {
	const caPEM = `-----BEGIN CERTIFICATE-----
dGhpcyBpcyBub3QgYSByZWFsIGNlcnRpZmljYXRl
-----END CERTIFICATE-----
`

	cases := map[string]struct {
		tls     *RegistryTLS
		wantErr string
	}{
		"no tls at all is fine": {},
		"an inline CA is fine": {
			tls: &RegistryTLS{CA: caPEM},
		},
		"a CA path is fine": {
			tls: &RegistryTLS{CAFile: "/etc/pki/ca.crt"},
		},
		"a client certificate needs its key": {
			tls:     &RegistryTLS{CertFile: "/etc/pki/client.crt"},
			wantErr: `registry "nexus.example.com": tls.certFile and tls.keyFile go together`,
		},
		"two ways to give the same CA": {
			tls:     &RegistryTLS{CA: caPEM, CAFile: "/etc/pki/ca.crt"},
			wantErr: `registry "nexus.example.com": set tls.ca or tls.caFile, not both`,
		},
		"a CA that is not a certificate": {
			tls:     &RegistryTLS{CA: "/etc/pki/ca.crt"},
			wantErr: `registry "nexus.example.com": tls.ca is not a PEM-encoded certificate`,
		},
		"a vault-encrypted CA is still ciphertext here": {
			tls: &RegistryTLS{CA: "$ANSIBLE_VAULT;1.1;AES256\n62306432326630316632\n"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.tls.Validate("nexus.example.com")
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

// A proxy address is completed to https when it was written without a scheme, and left alone
// when it already carries one.
func TestZarfClusterRegistriesMirrorEndpoint(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"mirror.example.com":              "https://mirror.example.com",
		"mirror.example.com:5000":         "https://mirror.example.com:5000",
		"mirror.example.com:5000/v2":      "https://mirror.example.com:5000/v2",
		"https://mirror.example.com:5000": "https://mirror.example.com:5000",
		"http://mirror.example.com":       "http://mirror.example.com",
	}
	for url, want := range cases {
		registry := Registries{Name: "docker.io"}
		if url != "" {
			registry.Proxy = &RegistryProxy{URL: url}
		}
		require.Equal(t, want, registry.MirrorEndpoint(), "proxy url %q", url)
	}
}

// An engine matches its per-registry configuration on the host alone, so a proxy address with a
// scheme or a path comes back reduced to host[:port]. Without a proxy, the registry itself is
// the host the credentials belong to.
func TestZarfClusterRegistriesConfigHost(t *testing.T) {
	cases := map[string]string{
		"":                                   "docker.io",
		"mirror.example.com":                 "mirror.example.com",
		"mirror.example.com:5000":            "mirror.example.com:5000",
		"https://mirror.example.com:5000":    "mirror.example.com:5000",
		"https://mirror.example.com/v2":      "mirror.example.com",
		"http://mirror.example.com:5000/v2/": "mirror.example.com:5000",
		"mirror.example.com/ghcr":            "mirror.example.com",
	}
	for url, want := range cases {
		registry := Registries{Name: "docker.io"}
		if url != "" {
			registry.Proxy = &RegistryProxy{URL: url}
		}
		require.Equal(t, want, registry.ConfigHost(), "proxy url %q", url)
	}
}

func TestValidateRegistries(t *testing.T) {
	const caPEM = `-----BEGIN CERTIFICATE-----
dGhpcyBpcyBub3QgYSByZWFsIGNlcnRpZmljYXRl
-----END CERTIFICATE-----
`
	robot := RegistryAuth{Username: "robot", Password: "secretpassword"}

	cases := map[string]struct {
		registries []Registries
		wantErr    string
	}{
		"nothing configured is fine": {},
		"several registries behind one mirror, sharing its credentials": {
			registries: []Registries{
				{Name: "docker.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com"}, Authentication: robot},
				{Name: "ghcr.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com/ghcr"}, Authentication: robot},
			},
		},
		"the same registry twice": {
			registries: []Registries{
				{Name: "docker.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com"}},
				{Name: "docker.io", Proxy: &RegistryProxy{URL: "https://other.example.com"}},
			},
			wantErr: `registry "docker.io": listed more than once`,
		},
		"one mirror, two sets of credentials": {
			registries: []Registries{
				{Name: "docker.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com"}, Authentication: robot},
				{Name: "ghcr.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com/ghcr"}, Authentication: RegistryAuth{Token: "tok"}},
			},
			wantErr: `registries "docker.io" and "ghcr.io" both configure host "mirror.example.com" with different auth or tls settings`,
		},
		"one mirror, two sets of tls settings": {
			registries: []Registries{
				{Name: "docker.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com"}, TLS: &RegistryTLS{CA: caPEM}},
				{Name: "ghcr.io", Proxy: &RegistryProxy{URL: "https://mirror.example.com/ghcr"}, TLS: &RegistryTLS{InsecureSkipVerify: true}},
			},
			wantErr: `registries "docker.io" and "ghcr.io" both configure host "mirror.example.com" with different auth or tls settings`,
		},
		"a bad entry is still reported": {
			registries: []Registries{{Name: "docker.io"}},
			wantErr:    `registry "docker.io": needs at least one of proxy.url, auth, or tls`,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateRegistries(tc.registries)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, tc.wantErr)
		})
	}
}
