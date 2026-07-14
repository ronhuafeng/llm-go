package codexsdk

import (
	"encoding/json"

	"github.com/ronhuafeng/codexsdk-go/codexsdk/protocolv2"
)

// notificationAttributionClass records the per-run evidence rule derived from
// the generated notification payload schema. Thread-scoped facts attach only
// to runs that are active (including attachment) for that thread when the fact
// is ingested; they are never saved for a subsequent run.
type notificationAttributionClass uint8

const (
	notificationAttributionUnsupported notificationAttributionClass = iota
	notificationAttributionTurn
	notificationAttributionThread
	notificationAttributionGlobal
)

type notificationIdentity struct {
	threadID string
	turnID   string
}

var notificationAttribution = func() map[protocolv2.ServerNotificationKind]notificationAttributionClass {
	classes := make(map[protocolv2.ServerNotificationKind]notificationAttributionClass)
	add := func(class notificationAttributionClass, methods ...string) {
		for _, method := range methods {
			classes[protocolv2.ServerNotificationKind(method)] = class
		}
	}
	add(notificationAttributionTurn,
		"error", "item/agentMessage/delta",
		"item/autoApprovalReview/completed", "item/autoApprovalReview/started",
		"item/commandExecution/outputDelta", "item/commandExecution/terminalInteraction",
		"item/completed", "item/fileChange/outputDelta", "item/fileChange/patchUpdated",
		"item/mcpToolCall/progress", "item/plan/delta", "item/reasoning/summaryPartAdded",
		"item/reasoning/summaryTextDelta", "item/reasoning/textDelta", "item/started",
		"model/rerouted", "model/safetyBuffering/updated", "model/verification",
		"thread/compacted", "thread/tokenUsage/updated", "turn/completed",
		"turn/diff/updated", "turn/moderationMetadata", "turn/plan/updated", "turn/started",
	)
	add(notificationAttributionThread,
		"guardianWarning", "hook/completed", "hook/started", "serverRequest/resolved",
		"thread/archived", "thread/closed", "thread/deleted", "thread/goal/cleared",
		"thread/goal/updated", "thread/name/updated", "thread/realtime/closed",
		"thread/realtime/error", "thread/realtime/itemAdded", "thread/realtime/outputAudio/delta",
		"thread/realtime/sdp", "thread/realtime/started", "thread/realtime/transcript/delta",
		"thread/realtime/transcript/done", "thread/settings/updated", "thread/status/changed",
		"thread/unarchived",
	)
	add(notificationAttributionGlobal,
		"account/login/completed", "account/rateLimits/updated", "account/updated",
		"app/list/updated", "command/exec/outputDelta", "configWarning", "deprecationNotice",
		"externalAgentConfig/import/completed", "externalAgentConfig/import/progress", "fs/changed",
		"fuzzyFileSearch/sessionCompleted", "fuzzyFileSearch/sessionUpdated",
		"mcpServer/oauthLogin/completed", "mcpServer/startupStatus/updated", "process/exited", "process/outputDelta",
		"remoteControl/status/changed", "skills/changed", "thread/started", "warning",
		"windows/worldWritableWarning", "windowsSandbox/setupCompleted",
	)
	return classes
}()

func attributionFor(notification protocolv2.ServerNotification) (notificationAttributionClass, notificationIdentity) {
	class := attributionClassForKind(notification.Kind())
	if class == notificationAttributionUnsupported {
		return notificationAttributionUnsupported, notificationIdentity{}
	}
	raw, err := json.Marshal(notification)
	if err != nil {
		return notificationAttributionUnsupported, notificationIdentity{}
	}
	var envelope struct {
		Params struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		} `json:"params"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return notificationAttributionUnsupported, notificationIdentity{}
	}
	if envelope.Params.TurnID == "" {
		envelope.Params.TurnID = envelope.Params.Turn.ID
	}
	return class, notificationIdentity{threadID: envelope.Params.ThreadID, turnID: envelope.Params.TurnID}
}

func attributionClassForKind(kind protocolv2.ServerNotificationKind) notificationAttributionClass {
	if class, ok := notificationAttribution[kind]; ok {
		return class
	}
	return notificationAttributionUnsupported
}
