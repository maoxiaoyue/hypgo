package schema

// RegisterGRPC 註冊 gRPC 服務方法的 schema
// command 格式："ServiceName/MethodName"
func RegisterGRPC(command, summary string, input, output interface{}) {
	route := Route{
		Protocol: "grpc",
		Command:  command,
		Summary:  summary,
		Input:    input,
		Output:   output,
	}
	if input != nil {
		route.InputName = TypeName(input)
	}
	if output != nil {
		route.OutputName = TypeName(output)
	}
	Global().Register(route)
}

// RegisterBot 註冊通用 bot command schema（跨平台）
func RegisterBot(command, summary string, input, output interface{}) {
	route := Route{
		Protocol: "bot",
		Command:  command,
		Summary:  summary,
		Input:    input,
		Output:   output,
	}
	if input != nil {
		route.InputName = TypeName(input)
	}
	if output != nil {
		route.OutputName = TypeName(output)
	}
	Global().Register(route)
}

// RegisterBotPlatform 註冊特定平台的 bot command schema
// platform: "telegram", "line", "discord", "whatsapp", "slack" 等
func RegisterBotPlatform(platform, command, summary string, input, output interface{}) {
	route := Route{
		Protocol: "bot",
		Platform: platform,
		Command:  command,
		Summary:  summary,
		Input:    input,
		Output:   output,
		Tags:     []string{platform},
	}
	if input != nil {
		route.InputName = TypeName(input)
	}
	if output != nil {
		route.OutputName = TypeName(output)
	}
	Global().Register(route)
}

// RegisterMCP 註冊 MCP (Model Context Protocol) tool schema
func RegisterMCP(command, summary string, input, output interface{}) {
	route := Route{
		Protocol: "mcp",
		Command:  command,
		Summary:  summary,
		Input:    input,
		Output:   output,
	}
	if input != nil {
		route.InputName = TypeName(input)
	}
	if output != nil {
		route.OutputName = TypeName(output)
	}
	Global().Register(route)
}

// RegisterWebSocket 註冊 WebSocket 訊息類型的 schema
func RegisterWebSocket(command, summary string, input, output interface{}) {
	route := Route{
		Protocol: "websocket",
		Command:  command,
		Summary:  summary,
		Input:    input,
		Output:   output,
	}
	if input != nil {
		route.InputName = TypeName(input)
	}
	if output != nil {
		route.OutputName = TypeName(output)
	}
	Global().Register(route)
}

// RegisterCLI 註冊 CLI 子命令的 schema
func RegisterCLI(command, summary string, input, output interface{}) {
	route := Route{
		Protocol: "cli",
		Command:  command,
		Summary:  summary,
		Input:    input,
		Output:   output,
	}
	if input != nil {
		route.InputName = TypeName(input)
	}
	if output != nil {
		route.OutputName = TypeName(output)
	}
	Global().Register(route)
}
