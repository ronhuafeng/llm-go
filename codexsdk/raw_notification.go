package codexsdk

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"sync"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

// RawResponseItemCompletedExtension is an opt-in extension, absent from the
// selected baseline's public ServerNotification union. Its payload is validated
// against that baseline's standalone RawResponseItemCompletedNotification schema.
const RawResponseItemCompletedExtension = "rawResponseItem/completed"

var (
	ErrRawNotificationUnsupported = errors.New("codexsdk: required raw notification unsupported")
	ErrRawNotificationDecode      = errors.New("codexsdk: required raw notification invalid")
	ErrRawNotificationDelivery    = errors.New("codexsdk: required raw notification delivery failed")
)

// RawNotification preserves the complete JSON envelope, including unknown
// fields, exact number tokens and omitted versus null members. It is observation
// data, never authority to answer an action-bearing server request.
type RawNotification struct {
	Method   string
	ThreadID string
	TurnID   string
	Envelope protocolv2.JSONValue
}

// RawNotificationHandler observes an admitted frame. Error or panic fails the
// client. It runs serially with notification callbacks, outside the transport reader.
type RawNotificationHandler func(context.Context, RawNotification) error

// RawNotificationSubscription requires SDK delivery of the listed methods.
// Known generated server notifications are supported. The standalone raw-item
// method additionally requires EnableRawResponseItemExtension. Unsupported or
// duplicate methods and missing handlers fail construction before process launch.
// This subscription does not require the server to emit any particular event;
// applications decide whether their observations are sufficient.
type RawNotificationSubscription struct {
	Methods                        []string
	EnableRawResponseItemExtension bool
	Handler                        RawNotificationHandler
}

//go:embed internal/protocolschema/appserver/v2/v2/RawResponseItemCompletedNotification.json
var rawResponseItemSchemaBytes []byte

var rawResponseItemSchema = sync.OnceValues(func() (*validator.Schema, error) {
	var doc any
	if err := unmarshalJSONPreserveNumbers(rawResponseItemSchemaBytes, &doc); err != nil {
		return nil, ErrRawNotificationUnsupported
	}
	compiler := validator.NewCompiler()
	compiler.UseLoader(rawSchemaLoader{})
	const url = "https://codexsdk.invalid/raw-response-item.json"
	if err := compiler.AddResource(url, doc); err != nil {
		return nil, ErrRawNotificationUnsupported
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		return nil, ErrRawNotificationUnsupported
	}
	return schema, nil
})

type rawSchemaLoader struct{}

func (rawSchemaLoader) Load(string) (any, error) { return nil, ErrRawNotificationUnsupported }

func validateRawSubscription(options ClientOptions) (ClientOptions, error) {
	if options.RawNotifications == nil {
		return options, nil
	}
	subscription := *options.RawNotifications
	subscription.Methods = append([]string(nil), subscription.Methods...)
	if subscription.Handler == nil || len(subscription.Methods) == 0 {
		return ClientOptions{}, ErrRawNotificationUnsupported
	}
	seen := map[string]bool{}
	for _, method := range subscription.Methods {
		if seen[method] {
			return ClientOptions{}, ErrRawNotificationUnsupported
		}
		seen[method] = true
		if method == RawResponseItemCompletedExtension && subscription.EnableRawResponseItemExtension {
			if _, err := rawResponseItemSchema(); err != nil {
				return ClientOptions{}, err
			}
			continue
		}
		info, ok := protocolv2.LookupMethod(method)
		if !ok || info.Direction != protocolv2.MethodDirectionServerToClient || info.Kind != protocolv2.MethodKindNotification {
			return ClientOptions{}, ErrRawNotificationUnsupported
		}
	}
	options.RawNotifications = &subscription
	return options, nil
}

func (c *Client) requiresRawNotification(method string) bool {
	if c.options.RawNotifications != nil {
		for _, required := range c.options.RawNotifications.Methods {
			if required == method {
				return true
			}
		}
	}
	return false
}

func (c *Client) hasNotificationHandler() bool {
	return c.options.ServerNotificationHandler != nil || c.options.RawNotifications != nil
}

func rawNotification(notification rpcNotification) (*RawNotification, error) {
	if notification.method == RawResponseItemCompletedExtension {
		schema, err := rawResponseItemSchema()
		if err != nil {
			return nil, err
		}
		if schema.Validate(notification.params) != nil {
			return nil, ErrRawNotificationDecode
		}
	}
	envelope := notification.envelope
	if envelope == nil {
		envelope = map[string]any{"method": notification.method, "params": notification.params}
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, ErrRawNotificationDecode
	}
	value, err := protocolv2.ParseJSONValue(raw)
	if err != nil {
		return nil, ErrRawNotificationDecode
	}
	threadID := threadIDFromNotification(notification.params)
	if threadID == "" {
		thread, _ := notification.params["thread"].(map[string]any)
		threadID = jsonString(thread["id"])
	}
	return &RawNotification{Method: notification.method, ThreadID: threadID, TurnID: turnIDFromNotification(notification.params), Envelope: value}, nil
}

type rawHandlerError struct{ cause error }

func (e *rawHandlerError) Error() string { return ErrRawNotificationDelivery.Error() }
func (e *rawHandlerError) Unwrap() []error {
	causes := []error{ErrRawNotificationDelivery, ErrHandlerFailed}
	if e.cause != nil {
		causes = append(causes, e.cause)
	}
	return causes
}

func invokeRawNotificationHandler(ctx context.Context, handler RawNotificationHandler, notification RawNotification) (err error) {
	defer func() {
		if recover() != nil {
			err = &rawHandlerError{}
		}
	}()
	if cause := handler(ctx, notification); cause != nil {
		return &rawHandlerError{cause: cause}
	}
	return nil
}
