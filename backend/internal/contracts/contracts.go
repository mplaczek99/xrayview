package contracts

import contractv1 "xrayview/contracts/contractv1"

type CommandName string

const (
	CommandGetProcessingManifest CommandName = "get_processing_manifest"
	CommandOpenStudy             CommandName = "open_study"
	CommandStartRenderJob        CommandName = "start_render_job"
	CommandStartProcessJob       CommandName = "start_process_job"
	CommandStartAnalyzeJob       CommandName = "start_analyze_job"
	CommandGetJob                CommandName = "get_job"
	CommandGetJobs               CommandName = "get_jobs"
	CommandCancelJob             CommandName = "cancel_job"
	CommandMeasureLineAnnotation CommandName = "measure_line_annotation"
)

var SupportedCommands = []CommandName{
	CommandGetProcessingManifest,
	CommandOpenStudy,
	CommandStartRenderJob,
	CommandStartProcessJob,
	CommandStartAnalyzeJob,
	CommandGetJob,
	CommandGetJobs,
	CommandCancelJob,
	CommandMeasureLineAnnotation,
}

func SupportedCommandStrings() []string {
	names := make([]string, 0, len(SupportedCommands))
	for _, command := range SupportedCommands {
		names = append(names, string(command))
	}

	return names
}

func IsSupportedCommand(name string) bool {
	for _, command := range SupportedCommands {
		if string(command) == name {
			return true
		}
	}

	return false
}

const (
	ServiceName             = "xrayview-backend"
	BackendContractVersion  = contractv1.BackendContractVersion
	BackendContractSchemaID = contractv1.BackendContractSchemaID
)
