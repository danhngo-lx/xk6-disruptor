// Package version provide information about the build version
package version

import (
	"os"
	"runtime/debug"
)

const xk6DisruptorPath = "github.com/danhngo-lx/xk6-disruptor"

// agentImageRepo is the container registry and image name for the agent.
// It can be overridden at build time with:
//
//	-ldflags "-X github.com/danhngo-lx/xk6-disruptor/pkg/internal/version.agentImageRepo=ghcr.io/myorg/xk6-disruptor-agent"
var agentImageRepo = "ghcr.io/danhngo-lx/xk6-disruptor-agent" //nolint:gochecknoglobals

// AgentImageEnvVar is the environment variable that overrides the agent image at runtime.
// The value should be a full image reference including registry, name, and tag,
// e.g. "ghcr.io/myorg/xk6-disruptor-agent:v1.2.3".
// When set, it takes precedence over both the build-time default and the version-derived tag.
const AgentImageEnvVar = "XK6_DISRUPTOR_AGENT_IMAGE"

// DisruptorVersion returns the version of the currently executed disruptor
func DisruptorVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == xk6DisruptorPath {
				if d.Replace != nil {
					return d.Replace.Version
				}
				return d.Version
			}
		}
	}

	return ""
}

// AgentImage returns the container image reference for the disruptor agent.
//
// Resolution order (first non-empty value wins):
//  1. XK6_DISRUPTOR_AGENT_IMAGE environment variable (full image reference)
//  2. agentImageRepo build-time variable + version-derived tag
//  3. agentImageRepo build-time variable + "latest"
func AgentImage() string {
	// Runtime override takes full precedence
	if img := os.Getenv(AgentImageEnvVar); img != "" {
		return img
	}

	tag := "latest"

	// if a specific version of the disruptor was built, use it for agent's tag
	// (go test sets version to "")
	dv := DisruptorVersion()
	if dv != "" && dv != "(devel)" {
		tag = dv
	}

	return agentImageRepo + ":" + tag
}
