package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataRoot(t *testing.T) {
	tests := []struct {
		name    string
		opts    ResolveOpts
		want    string
		wantErr bool
	}{
		{
			name: "macos application support",
			opts: ResolveOpts{GOOS: "darwin", Home: "/Users/alice"},
			want: filepath.Join("/Users/alice", "Library", "Application Support", AppName),
		},
		{
			name: "linux default xdg share",
			opts: ResolveOpts{GOOS: "linux", Home: "/home/alice"},
			want: filepath.Join("/home/alice", ".local", "share", AppName),
		},
		{
			name: "linux xdg data home",
			opts: ResolveOpts{GOOS: "linux", Home: "/home/alice", XDGDataHome: "/custom/data"},
			want: filepath.Join("/custom/data", AppName),
		},
		{
			name: "portable flag",
			opts: ResolveOpts{GOOS: "darwin", Home: "/Users/alice", ExeDir: "/opt/app", PortableFlag: true},
			want: filepath.Join("/opt/app", portableDataDirName),
		},
		{
			name: "portable marker",
			opts: ResolveOpts{GOOS: "linux", Home: "/home/alice", ExeDir: "/opt/app", PortableMarker: true},
			want: filepath.Join("/opt/app", portableDataDirName),
		},
		{
			name:    "portable missing exe dir",
			opts:    ResolveOpts{PortableFlag: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DataRoot(tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DataRoot: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultSettings(t *testing.T) {
	tests := []struct {
		name string
		got  AppSettings
		want AppSettings
	}{
		{
			name: "product defaults",
			got:  DefaultSettings(),
			want: AppSettings{
				LocalPort:  DefaultPort,
				RealModel:  DefaultRealModel,
				AliasModel: DefaultAliasModel,
				TunnelMode: DefaultTunnelMode,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestEnsureDataRoot(t *testing.T) {
	tmp := t.TempDir()
	root, err := EnsureDataRoot(ResolveOpts{
		GOOS: "linux",
		Home: tmp,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tmp, ".local", "share", AppName)
	if root != want {
		t.Fatalf("root=%q want=%q", root, want)
	}
	if _, err := os.Stat(filepath.Join(root, "data")); err != nil {
		t.Fatalf("data subdir missing: %v", err)
	}
}
