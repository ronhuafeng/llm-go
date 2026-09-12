package codexsdk

import (
	"encoding/json"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
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

func attributionFor(notification protocolv2.ServerNotification) (notificationAttributionClass, notificationIdentity) {
	if !knownServerNotificationKind(notification.Kind()) {
		return notificationAttributionUnsupported, notificationIdentity{}
	}
	identity := extractNotificationIdentity(notification)
	if identity.turnID != "" {
		return notificationAttributionTurn, identity
	}
	if identity.threadID != "" {
		return notificationAttributionThread, identity
	}
	return notificationAttributionGlobal, identity
}

func attributionClassForKind(kind protocolv2.ServerNotificationKind) notificationAttributionClass {
	if !knownServerNotificationKind(kind) {
		return notificationAttributionUnsupported
	}
	return notificationAttributionGlobal
}

func knownServerNotificationKind(kind protocolv2.ServerNotificationKind) bool {
	info, ok := protocolv2.LookupMethod(string(kind))
	return ok && info.Direction == protocolv2.MethodDirectionServerToClient && info.Kind == protocolv2.MethodKindNotification
}

func extractNotificationIdentity(notification protocolv2.ServerNotification) notificationIdentity {
	raw, err := json.Marshal(notification)
	if err != nil {
		return notificationIdentity{}
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return notificationIdentity{}
	}
	params, _ := envelope["params"].(map[string]any)
	if params == nil {
		params = envelope
	}
	identity := notificationIdentity{
		threadID: jsonString(params["threadId"]),
		turnID:   jsonString(params["turnId"]),
	}
	if identity.turnID == "" {
		if turn, ok := params["turn"].(map[string]any); ok {
			identity.turnID = jsonString(turn["id"])
		}
	}
	if identity.threadID == "" {
		if thread, ok := params["thread"].(map[string]any); ok {
			identity.threadID = jsonString(thread["id"])
		}
	}
	return identity
}

func jsonString(value any) string {
	text, _ := value.(string)
	return text
}
