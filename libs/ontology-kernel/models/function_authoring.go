package models

// FunctionAuthoringTemplate mirrors `struct FunctionAuthoringTemplate`
// in `libs/ontology-kernel/src/models/function_authoring.rs`.
type FunctionAuthoringTemplate struct {
	ID                   string               `json:"id"`
	Runtime              string               `json:"runtime"`
	DisplayName          string               `json:"display_name"`
	Description          string               `json:"description"`
	Entrypoint           string               `json:"entrypoint"`
	StarterSource        string               `json:"starter_source"`
	DefaultCapabilities  FunctionCapabilities `json:"default_capabilities"`
	RecommendedUseCases  []string             `json:"recommended_use_cases"`
	CLIScaffoldTemplate  *string              `json:"cli_scaffold_template"`
	SDKPackages          []string             `json:"sdk_packages"`
}

// FunctionSDKPackageReference mirrors `struct FunctionSdkPackageReference`.
type FunctionSDKPackageReference struct {
	Language    string `json:"language"`
	Path        string `json:"path"`
	PackageName string `json:"package_name"`
	GeneratedBy string `json:"generated_by"`
}

// FunctionAuthoringSurfaceResponse mirrors
// `struct FunctionAuthoringSurfaceResponse`.
type FunctionAuthoringSurfaceResponse struct {
	Templates   []FunctionAuthoringTemplate   `json:"templates"`
	SDKPackages []FunctionSDKPackageReference `json:"sdk_packages"`
	CLICommands []string                      `json:"cli_commands"`
}
