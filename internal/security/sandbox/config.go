package sandbox

// Config décrit la configuration d'un backend sandbox.
type Config struct {
	Enabled        bool          `yaml:"enabled"`
	Backend        string        `yaml:"backend"` // none | chroot | bwrap
	Rootfs         string        `yaml:"rootfs"`
	Workdir        string        `yaml:"workdir"`
	ReadonlyBinds  []string      `yaml:"readonly_binds"`
	ReadwriteBinds []BindMount   `yaml:"readwrite_binds"`
	Tmpfs          []string      `yaml:"tmpfs"`
	Symlinks       []SymlinkSpec `yaml:"symlinks"`
	Unshare        Unshare       `yaml:"unshare"`
	DieWithParent  bool          `yaml:"die_with_parent"`
	UID            *uint32       `yaml:"uid,omitempty"`
	GID            *uint32       `yaml:"gid,omitempty"`
}

// BindMount décrit un montage bind avec source et cible distincts.
type BindMount struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

// SymlinkSpec décrit un lien symbolique à créer dans le namespace bwrap.
// Source est la valeur du lien (peut être relatif), Target le chemin dans le namespace.
type SymlinkSpec struct {
	Source string `yaml:"source"` // ex: "usr/bin"
	Target string `yaml:"target"` // ex: "/bin"
}

// Unshare liste les namespaces Linux à isoler.
type Unshare struct {
	Network bool `yaml:"network"`
	PID     bool `yaml:"pid"`
	IPC     bool `yaml:"ipc"`
	UTS     bool `yaml:"uts"`
	User    bool `yaml:"user"`
}

// DefaultConfig retourne une configuration désactivée (backend none).
func DefaultConfig() Config {
	return Config{Enabled: false, Backend: "none"}
}
