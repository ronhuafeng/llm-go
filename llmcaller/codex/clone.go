package codexcaller

import (
	"encoding/json"
	"reflect"

	"github.com/ronhuafeng/llm-go/codexsdk"
	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func cloneStartRequest(request codexsdk.StartThreadRunRequest) (codexsdk.StartThreadRunRequest, error) {
	var cloned codexsdk.StartThreadRunRequest
	if err := cloneGenerated(request.Thread, &cloned.Thread); err != nil {
		return cloned, err
	}
	turn := request.Turn
	nilInput := turn.Input == nil
	if nilInput {
		turn.Input = []protocolv2.UserInput{}
	}
	if err := cloneGenerated(turn, &cloned.Turn); err != nil {
		return cloned, err
	}
	if nilInput {
		cloned.Turn.Input = nil
	}
	cloned.AdmitTurn = request.AdmitTurn
	return cloned, nil
}

func cloneStartedRun(run codexsdk.StartedThreadRun) (codexsdk.StartedThreadRun, error) {
	var cloned codexsdk.StartedThreadRun
	if !reflect.DeepEqual(run.Start, protocolv2.ThreadStartResponse{}) {
		if err := cloneGeneratedOrSafeValue(run.Start, &cloned.Start); err != nil {
			return cloned, err
		}
	}
	cloned.Run = run.Run
	if hasTurnEvidence(run.Run.Turn) {
		if err := cloneGeneratedOrSafeValue(run.Run.Turn, &cloned.Run.Turn); err != nil {
			return cloned, err
		}
	}
	if run.Run.Usage != nil {
		var usage protocolv2.ThreadTokenUsage
		if err := cloneGenerated(*run.Run.Usage, &usage); err != nil {
			return cloned, err
		}
		cloned.Run.Usage = &usage
	}
	if run.Run.Notifications != nil {
		cloned.Run.Notifications = make([]protocolv2.ServerNotification, len(run.Run.Notifications))
		for index := range run.Run.Notifications {
			if err := cloneGenerated(run.Run.Notifications[index], &cloned.Run.Notifications[index]); err != nil {
				return cloned, err
			}
		}
	}
	cloned.Run.Diagnostics = append([]codexsdk.DiagnosticRef(nil), run.Run.Diagnostics...)
	return cloned, nil
}

func hasTurnEvidence(turn protocolv2.Turn) bool {
	return turn.CompletedAt != nil ||
		turn.DurationMS != nil ||
		turn.Error != nil ||
		turn.ID != "" ||
		turn.Items != nil ||
		turn.ItemsView != nil ||
		turn.StartedAt != nil ||
		turn.Status != ""
}

func cloneGenerated(source any, destination any) error {
	raw, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, destination)
}

func cloneGeneratedOrSafeValue(source any, destination any) error {
	if err := cloneGenerated(source, destination); err != nil {
		if hasMutableReferences(reflect.ValueOf(source)) {
			return err
		}
		reflect.ValueOf(destination).Elem().Set(reflect.ValueOf(source))
	}
	return nil
}

func hasMutableReferences(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Interface:
		return !value.IsNil() && hasMutableReferences(value.Elem())
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	case reflect.UnsafePointer:
		return !value.IsNil()
	case reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if hasMutableReferences(value.Field(index)) {
				return true
			}
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if hasMutableReferences(value.Field(index)) {
				return true
			}
		}
	}
	return false
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
