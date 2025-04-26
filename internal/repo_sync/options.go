package repo_sync

import "time"

type Options struct {
    HTTPTimeout time.Duration
    Debug       bool
    Verbose     bool
    Jobs           int
    JobsNetwork    int
    JobsCheckout   int
    CurrentBranch  bool
    Detach         bool
    ForceSync      bool
    ForceRemoveDirty bool
    ForceOverwrite bool
    LocalOnly      bool
    NetworkOnly    bool
    Prune          bool
    Quiet          bool
    SmartSync      bool
    Tags           bool
    NoTags         bool
    NoCloneBundle  bool
    FetchSubmodules bool
    OptimizedFetch bool
    RetryFetches   int
    Groups         []string
    FailFast       bool
    NoManifestUpdate bool
    UseSuperproject bool
    HyperSync      bool
    SmartTag       string
    OuterManifest  bool
    ThisManifestOnly bool
    ManifestServerUsername string
    ManifestServerPassword string
    Depth           int
}