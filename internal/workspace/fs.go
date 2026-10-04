package workspace

// Entry is one item in a workspace listing.
type Entry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// FS is the storage the tools operate on. Implementations must confine every
// operation to their own root and must be safe for concurrent use.
//
// All paths are model-supplied and untrusted; implementations call Clean
// themselves rather than trusting callers to have done so.
type FS interface {
	// ReadFile returns the contents of a file.
	ReadFile(name string) ([]byte, error)
	// WriteFile creates or replaces a file, creating parent directories.
	WriteFile(name string, data []byte) error
	// Exists reports whether a file (not a directory) exists at name.
	Exists(name string) (bool, error)
	// List returns everything under dir, recursively, in lexical order.
	List(dir string) ([]Entry, error)
}

// Limits bound what a workspace will hold. Zero values mean "no limit".
type Limits struct {
	MaxFiles      int   // number of files
	MaxFileBytes  int64 // size of any one file, read or written
	MaxTotalBytes int64 // sum of all file sizes (MemFS only)
	MaxEntries    int   // entries returned by one List call
}

// DemoLimits are the quotas for a visitor's workspace on the public site.
var DemoLimits = Limits{MaxFiles: 40, MaxFileBytes: 64 << 10, MaxTotalBytes: 512 << 10, MaxEntries: 200}

// LocalLimits are the defaults for the CLI working on a real directory.
var LocalLimits = Limits{MaxFileBytes: 1 << 20, MaxEntries: 2000}
