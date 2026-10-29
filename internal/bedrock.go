package internal

import "github.com/sandertv/gophertunnel/minecraft/protocol"

const (
	// BedrockProtocolVersion is the supported Minecraft Bedrock protocol ID.
	BedrockProtocolVersion = protocol.CurrentProtocol
	// BedrockGameVersion is the supported Minecraft Bedrock version string.
	BedrockGameVersion = protocol.CurrentVersion
)
