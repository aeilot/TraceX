// Package agentsysbench ships the AgentSysBench adapter that the experiment
// runner copies into each private job directory: the job driver, the
// unified-schema converter, the cgroup sampler and the lossless checker.
package agentsysbench

import "embed"

// Files holds the scripts by their file names.
//
//go:embed driver.sh asb2tracex.py sample_cgroups.py check_lossless.py
var Files embed.FS

// Names lists every embedded script, in the order they are written to a job.
var Names = []string{"driver.sh", "asb2tracex.py", "sample_cgroups.py", "check_lossless.py"}
