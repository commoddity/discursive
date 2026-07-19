package config

import "testing"

func TestNormalizePublicBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "host_only", in: "https://ai.example.com", want: "https://ai.example.com/v1"},
		{name: "with_v1", in: "https://ai.example.com/v1", want: "https://ai.example.com/v1"},
		{name: "trailing_slash", in: "https://ai.example.com/v1/", want: "https://ai.example.com/v1"},
		{name: "localhost", in: "https://localhost/v1", wantErr: true},
		{name: "loopback_ip", in: "https://127.0.0.1/v1", wantErr: true},
		{name: "http", in: "http://ai.example.com/v1", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePublicBaseURL(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestValidateTunnelSettings(t *testing.T) {
	tests := []struct {
		name    string
		s       AppSettings
		wantErr bool
	}{
		{
			name: "named_ok",
			s: AppSettings{
				TunnelMode:           TunnelModeNamed,
				PublicBaseURL:        "https://ai.example.com/v1",
				TunnelTokenEncrypted: strPtr("enc"),
			},
		},
		{
			name: "named_missing_token",
			s: AppSettings{
				TunnelMode:    TunnelModeNamed,
				PublicBaseURL: "https://ai.example.com/v1",
			},
			wantErr: true,
		},
		{
			name: "named_missing_url",
			s: AppSettings{
				TunnelMode:           TunnelModeNamed,
				TunnelTokenEncrypted: strPtr("enc"),
			},
			wantErr: true,
		},
		{
			name: "none_ok",
			s: AppSettings{
				TunnelMode:    TunnelModeNone,
				PublicBaseURL: "https://ai.example.com/v1",
			},
		},
		{
			name: "none_missing_url",
			s: AppSettings{
				TunnelMode: TunnelModeNone,
			},
			wantErr: true,
		},
		{
			name: "quick_ok",
			s:    AppSettings{TunnelMode: TunnelModeQuick},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTunnelSettings(tt.s)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNeedsSetup(t *testing.T) {
	enc := strPtr("enc")
	tests := []struct {
		name string
		s    AppSettings
		want bool
	}{
		{
			name: "fresh_defaults",
			s:    DefaultSettings(),
			want: true,
		},
		{
			name: "keys_only",
			s: AppSettings{
				TunnelMode:           TunnelModeNamed,
				MoonshotKeyEncrypted: enc,
				DeepSeekKeyEncrypted: enc,
			},
			want: true,
		},
		{
			name: "named_complete",
			s: AppSettings{
				TunnelMode:           TunnelModeNamed,
				PublicBaseURL:        "https://ai.example.com/v1",
				TunnelTokenEncrypted: enc,
				MoonshotKeyEncrypted: enc,
				DeepSeekKeyEncrypted: enc,
			},
			want: false,
		},
		{
			name: "quick_with_keys",
			s: AppSettings{
				TunnelMode:           TunnelModeQuick,
				MoonshotKeyEncrypted: enc,
				DeepSeekKeyEncrypted: enc,
			},
			want: false,
		},
		{
			name: "quick_missing_deepseek",
			s: AppSettings{
				TunnelMode:           TunnelModeQuick,
				MoonshotKeyEncrypted: enc,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsSetup(tt.s); got != tt.want {
				t.Fatalf("NeedsSetup=%v want %v", got, tt.want)
			}
		})
	}
}

func TestTunnelTokenRoundTrip(t *testing.T) {
	dataRoot := t.TempDir()
	s := DefaultSettings()
	token := "eyJhIjoiZm9vIn0.test"
	if err := s.SetTunnelToken(dataRoot, token); err != nil {
		t.Fatal(err)
	}
	if !s.HasTunnelToken() {
		t.Fatal("expected has tunnel token")
	}
	got, err := s.GetTunnelToken(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != token {
		t.Fatalf("got %v want %q", got, token)
	}
}

func strPtr(s string) *string { return &s }
