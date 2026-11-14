package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inprobe"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// install_toolserver.go is the tool server's health wait, shared by `villa install
// --tool-server` (ADR-0042) and, through toolsHealthURL, the status rows. The contract
// proof (the bearer, the identity header, the registration, one real call) is `villa verify
// tools`, not this: install only needs to know each rendered unit is up before it reports.

const (
	// toolServerReadyTimeout bounds the wait after the units start. Each is one small Go
	// process in a distroless image, so a unit that has not answered in a minute is broken.
	toolServerReadyTimeout = 60 * time.Second
	toolServerPollInterval = 2 * time.Second
)

// toolsHealthURL is one unit's /healthz in its own network. The route is unauthenticated
// and answers {"status":"ok"}, so the health probe sends no credential.
func toolsHealthURL(addr string, port int) string {
	return "http://" + net.JoinHostPort(addr, strconv.Itoa(port)) + "/healthz"
}

// toolServerProbes is one health probe per unit. Each runs on the network its unit joins,
// so a probe that reaches the unit also shows the network placement it was rendered with.
type toolServerProbes struct {
	Web    func(context.Context) status.HealthState
	Recall func(context.Context) status.HealthState
}

// liveToolServerProbes wires each probe to its unit's own network: the web unit is on the
// routed network and the recall unit is on the closed one.
func liveToolServerProbes() toolServerProbes {
	return toolServerProbes{
		Web: func(context.Context) status.HealthState {
			return statusProber(routedNetwork).Coded(toolsHealthURL(config.ToolsWebAddr, config.ToolsWebPort))
		},
		Recall: func(context.Context) status.HealthState {
			return statusProber(closedNetwork).Coded(toolsHealthURL(config.ToolsRecallAddr, config.ToolsRecallPort))
		},
	}
}

// The status rows' probes are TTL-bounded like every other in-network probe, because each
// one starts a helper container.
var (
	toolsWebHealthCache    = &inprobe.Cache{TTL: memoryHealthTTL}
	toolsRecallHealthCache = &inprobe.Cache{TTL: memoryHealthTTL}
)

// liveToolsWebHealth and liveToolsRecallHealth are the two status rows' probes: the
// unit's own /healthz, asked on the network the unit joins.
func liveToolsWebHealth() status.HealthState {
	return toolsWebHealthCache.Get(func() status.HealthState {
		return liveToolServerProbes().Web(context.Background())
	})
}

func liveToolsRecallHealth() status.HealthState {
	return toolsRecallHealthCache.Get(func() status.HealthState {
		return liveToolServerProbes().Recall(context.Background())
	})
}

// waitToolServer polls each unit cfg renders until it answers or the deadline passes. A unit
// that is confidently down at the deadline is a FAIL, which refuses the install and rolls it
// back. A unit whose probe could not run, or that is still loading, is a WARN: an unevaluable
// signal is never reported as a confident failure.
func waitToolServer(ctx context.Context, cfg config.VillaConfig, p toolServerProbes, interval, timeout time.Duration) install.Proof {
	type unit struct {
		name  string
		probe func(context.Context) status.HealthState
		state status.HealthState
	}
	var units []*unit
	if subsystem.ToolWebOn(cfg) {
		units = append(units, &unit{name: config.ToolsWebAddr, probe: p.Web})
	}
	if subsystem.ToolRecallOn(cfg) {
		units = append(units, &unit{name: config.ToolsRecallAddr, probe: p.Recall})
	}
	if len(units) == 0 {
		return install.Proof{Status: preflight.StatusWarn, Detail: "no tool unit renders, so there is nothing to wait for"}
	}

	deadline := time.Now().Add(timeout)
	for {
		pending := 0
		for _, u := range units {
			if u.state == status.HealthReady {
				continue
			}
			u.state = u.probe(ctx)
			if u.state != status.HealthReady {
				pending++
			}
		}
		if pending == 0 || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return install.Proof{Status: preflight.StatusFail, Detail: "interrupted while waiting for the tool units to answer /healthz"}
		case <-time.After(interval):
		}
	}

	var ready, down, unsure []string
	for _, u := range units {
		switch u.state {
		case status.HealthReady:
			ready = append(ready, u.name)
		case status.HealthDown:
			down = append(down, u.name)
		default:
			unsure = append(unsure, u.name+" ("+string(u.state)+")")
		}
	}
	switch {
	case len(down) > 0:
		return install.Proof{Status: preflight.StatusFail, Detail: strings.Join(down, ", ") + " did not answer /healthz in its network"}
	case len(unsure) > 0:
		return install.Proof{Status: preflight.StatusWarn, Detail: strings.Join(unsure, ", ") + " could not be confirmed up; run `villa status` to see it"}
	}
	return install.Proof{Status: preflight.StatusPass, Detail: strings.Join(ready, " and ") + " answered /healthz"}
}
