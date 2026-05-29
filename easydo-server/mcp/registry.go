package mcp

import (
	"context"
	"reflect"

	"easydo-server/internal/services"
)

type OperationType string

const (
	OperationRead  OperationType = "read"
	OperationWrite OperationType = "write"
)

type Tool struct {
	Name          string
	Description   string
	OperationType OperationType
	TargetType    string
	InputSchema   map[string]any
	Handler       ToolHandler
}

type ToolHandler func(context.Context, Invocation) (ToolResult, error)

type Invocation struct {
	Actor     any
	Arguments map[string]any
	RequestID string
	Protocol  string
}

type ToolResult struct {
	Content           any
	StructuredContent any
	Metadata          map[string]any
}

type Registry struct {
	tools map[string]Tool
	order []string
}

func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

func (r *Registry) Register(tool Tool) error {
	if tool.Name == "" {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "tool name is required"}
	}
	if tool.Handler == nil {
		return services.ServiceError{Code: services.ErrorCodeInvalidArgument, Message: "tool handler is required"}
	}
	if _, exists := r.tools[tool.Name]; exists {
		return services.ServiceError{Code: services.ErrorCodeConflict, Message: "tool already registered"}
	}
	tool.InputSchema = cloneJSONLikeMap(tool.InputSchema)
	r.tools[tool.Name] = tool
	r.order = append(r.order, tool.Name)
	return nil
}

func (r *Registry) List() []Tool {
	if len(r.order) == 0 {
		return nil
	}
	tools := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		tool, _ := r.Get(name)
		tools = append(tools, tool)
	}
	return tools
}

func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	if !ok {
		return Tool{}, false
	}
	tool.InputSchema = cloneJSONLikeMap(tool.InputSchema)
	return tool, true
}

func (r *Registry) Invoke(ctx context.Context, name string, invocation Invocation) (ToolResult, error) {
	tool, ok := r.tools[name]
	if !ok {
		return ToolResult{}, services.ServiceError{Code: services.ErrorCodeNotFound, Message: "tool not found"}
	}
	return tool.Handler(ctx, invocation)
}

func cloneJSONLikeMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = cloneJSONLikeValue(value)
	}
	return cloned
}

func cloneJSONLikeValue(value any) any {
	if value == nil {
		return nil
	}
	cloned, ok := cloneJSONLikeReflect(reflect.ValueOf(value))
	if !ok {
		return value
	}
	return cloned.Interface()
}

func cloneJSONLikeReflect(value reflect.Value) (reflect.Value, bool) {
	if !value.IsValid() {
		return reflect.Value{}, false
	}

	switch value.Kind() {
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return reflect.Value{}, false
		}
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(iter.Key(), cloneJSONLikeForType(iter.Value(), value.Type().Elem()))
		}
		return cloned, true
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneJSONLikeForType(value.Index(i), value.Type().Elem()))
		}
		return cloned, true
	case reflect.Array:
		cloned := reflect.New(value.Type()).Elem()
		for i := 0; i < value.Len(); i++ {
			cloned.Index(i).Set(cloneJSONLikeForType(value.Index(i), value.Type().Elem()))
		}
		return cloned, true
	default:
		return reflect.Value{}, false
	}
}

func cloneJSONLikeForType(value reflect.Value, target reflect.Type) reflect.Value {
	if !value.IsValid() {
		return reflect.Zero(target)
	}
	if value.Kind() == reflect.Interface && !value.IsNil() {
		cloned := cloneJSONLikeValue(value.Interface())
		return valueForType(cloned, target, value)
	}
	if cloned, ok := cloneJSONLikeReflect(value); ok {
		return valueForType(cloned.Interface(), target, value)
	}
	return valueForType(value.Interface(), target, value)
}

func valueForType(value any, target reflect.Type, fallback reflect.Value) reflect.Value {
	if value == nil {
		return reflect.Zero(target)
	}
	rv := reflect.ValueOf(value)
	if rv.Type().AssignableTo(target) {
		return rv
	}
	if rv.Type().ConvertibleTo(target) {
		return rv.Convert(target)
	}
	if fallback.IsValid() && fallback.Type().AssignableTo(target) {
		return fallback
	}
	return reflect.Zero(target)
}
