// SPDX-License-Identifier: Apache-2.0

package protocol

import (
	"strconv"
	"strings"
)

// PluginAPI is the major version of the protocol, sent as "pluginApi".
const PluginAPI = "1"

// Arvel extension methods (PROTOCOL §6, §7.9, §14).
const (
	MethodStatus        = "_arvel/status"
	MethodModels        = "_arvel/models"
	MethodUsage         = "_arvel/usage"
	MethodUsageUpdated  = "_arvel/usage_updated"
	MethodAction        = "_arvel/action"
	MethodStatusChanged = "_arvel/status_changed"
	MethodShutdown      = "_arvel/shutdown"
	MethodDiscover      = "_arvel/discover"
	MethodRoutePlan     = "_arvel/route/plan"
	MethodRouteMessage  = "_arvel/route/message"
	MethodRouteEffort   = "_arvel/route/effort"
	MethodRouteExplain  = "_arvel/route/explain"
)

// ACP methods used by the protocol.
const (
	MethodInitialize               = "initialize"
	MethodSessionNew               = "session/new"
	MethodSessionLoad              = "session/load"
	MethodSessionPrompt            = "session/prompt"
	MethodSessionCancel            = "session/cancel"
	MethodSessionSetConfigOption   = "session/set_config_option"
	MethodSessionUpdate            = "session/update"
	MethodSessionRequestPermission = "session/request_permission"
)

// Feature names listed in InitResultMeta.Features. "_arvel/interactive" and
// the feature "interactive" are reserved (PROTOCOL §7.8) and have no constant.
const (
	FeatureStatus       = "status"
	FeatureModels       = "models"
	FeatureUsage        = "usage"
	FeatureActions      = "actions"
	FeatureSubagents    = "subagents"
	FeatureResume       = "resume"
	FeatureSkills       = "skills"
	FeatureDiscover     = "discover"
	FeatureRoutePlan    = "route.plan"
	FeatureRouteMessage = "route.message"
	FeatureRouteEffort  = "route.effort"
	FeatureRouteExplain = "route.explain"
)

const maxParallelPrefix = "maxParallelSessions:"

// FeatureMaxParallelSessions returns the feature string that announces a soft
// limit of parallel sessions (PROTOCOL §8).
func FeatureMaxParallelSessions(n int) string {
	return maxParallelPrefix + strconv.Itoa(n)
}

// ParseMaxParallelSessions returns the first well-formed positive
// maxParallelSessions value found in features.
func ParseMaxParallelSessions(features []string) (int, bool) {
	for _, f := range features {
		rest, ok := strings.CutPrefix(f, maxParallelPrefix)
		if !ok {
			continue
		}
		n, err := strconv.Atoi(rest)
		if err != nil || n <= 0 {
			continue
		}
		return n, true
	}
	return 0, false
}
