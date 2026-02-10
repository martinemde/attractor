package main

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "attractor",
	Short: "Attractor pipeline runner",
	Long:  "Attractor runs DOT-defined pipelines against LLMs to drive agentic development workflows.",
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringP("model", "m", "claude-sonnet-4-5", "LLM model to use")
	rootCmd.PersistentFlags().String("provider", "", "Provider override (auto-detected from model if empty)")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose output")
	rootCmd.PersistentFlags().Bool("debug", false, "Debug output")
	rootCmd.PersistentFlags().String("aws-profile", "", "AWS shared config profile (e.g. SSO profile name)")

	_ = viper.BindPFlag("model", rootCmd.PersistentFlags().Lookup("model"))
	_ = viper.BindPFlag("provider", rootCmd.PersistentFlags().Lookup("provider"))
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
	_ = viper.BindPFlag("aws_profile", rootCmd.PersistentFlags().Lookup("aws-profile"))
}

func initConfig() {
	viper.SetEnvPrefix("ATTRACTOR")
	viper.AutomaticEnv()
}
