/*
 * Copyright 2021-2023 JetBrains s.r.o.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"fmt"
	"github.com/JetBrains/qodana-cli/v2023/cloud"
	"github.com/JetBrains/qodana-cli/v2023/platform"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"

	"github.com/JetBrains/qodana-cli/v2023/core"
	"github.com/spf13/cobra"
)

// newScanCommand returns a new instance of the scan command.
func newScanCommand() *cobra.Command {
	options := &platform.QodanaOptions{}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan project with Qodana",
		Long: `Scan a project with Qodana. It runs one of Qodana's Docker images (https://www.jetbrains.com/help/qodana/docker-images.html) and reports the results.

Note that most options can be configured via qodana.yaml (https://www.jetbrains.com/help/qodana/qodana-yaml.html) file.
But you can always override qodana.yaml options with the following command-line options.
`,
		Run: func(cmd *cobra.Command, args []string) {
			reportUrl := cloud.GetReportUrl(options.ResultsDir)

			ctx := cmd.Context()
			checkProjectDir(options.ProjectDir)
			options.FetchAnalyzerSettings()
			exitCode := core.RunAnalysis(ctx, &core.QodanaOptions{QodanaOptions: options})

			checkExitCode(exitCode, options.ResultsDir, options)
			core.ReadSarif(filepath.Join(options.ResultsDir, core.QodanaSarifName), options.PrintProblems)
			if platform.IsInteractive() {
				options.ShowReport = core.AskUserConfirm("Do you want to open the latest report")
			}

			newReportUrl := cloud.GetReportUrl(options.ResultsDir)
			if newReportUrl != reportUrl && newReportUrl != "" && !platform.IsContainer() {
				platform.SuccessMessage("Report is successfully uploaded to %s", newReportUrl)
			}

			if options.ShowReport {
				core.ShowReport(options.ResultsDir, options.ReportDir, options.Port)
			} else if !platform.IsContainer() && platform.IsInteractive() {
				platform.WarningMessage(
					"To view the Qodana report later, run %s in the current directory or add %s flag to %s",
					platform.PrimaryBold("qodana show"),
					platform.PrimaryBold("--show-report"),
					platform.PrimaryBold("qodana scan"),
				)
			}

			if exitCode == core.QodanaFailThresholdExitCode {
				platform.EmptyMessage()
				platform.ErrorMessage("The number of problems exceeds the fail threshold")
				os.Exit(exitCode)
			}
		},
	}

	err := platform.ComputeFlags(cmd, options)
	if err != nil {
		return nil
	}

	return cmd
}

func checkProjectDir(projectDir string) {
	if platform.IsInteractive() && core.IsHomeDirectory(projectDir) {
		platform.WarningMessage(
			fmt.Sprintf("Project directory (%s) is the $HOME directory", projectDir),
		)
		if !core.AskUserConfirm(platform.DefaultPromptText) {
			os.Exit(0)
		}
	}
	if !platform.CheckDirFiles(projectDir) {
		platform.ErrorMessage("No files to check with Qodana found in %s", projectDir)
		os.Exit(1)
	}
}

func checkExitCode(exitCode int, resultsDir string, options *core.QodanaOptions) {
	if exitCode == core.QodanaEapLicenseExpiredExitCode && platform.IsInteractive() {
		platform.EmptyMessage()
		platform.ErrorMessage(
			"Your license expired: update your license or token. If you are using EAP, make sure you are using the latest CLI version and update to the latest linter by running %s ",
			platform.PrimaryBold("qodana init"),
		)
		os.Exit(exitCode)
	} else if exitCode == core.QodanaTimeoutExitCodePlaceholder {
		core.ErrorMessage("Qodana analysis reached timeout %s", options.GetAnalysisTimeout())
		os.Exit(options.AnalysisTimeoutExitCode)
	} else if exitCode != core.QodanaSuccessExitCode && exitCode != core.QodanaFailThresholdExitCode {
		platform.ErrorMessage("Qodana exited with code %d", exitCode)
		platform.WarningMessage("Check ./logs/ in the results directory for more information")
		if exitCode == core.QodanaOutOfMemoryExitCode {
			core.CheckContainerEngineMemory()
		} else if core.AskUserConfirm(fmt.Sprintf("Do you want to open %s", resultsDir)) {
			err := core.OpenDir(resultsDir)
			if err != nil {
				log.Fatalf("Error while opening directory: %s", err)
			}
		}
		os.Exit(exitCode)
	}
}
