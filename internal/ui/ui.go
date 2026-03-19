package ui

import (
	"errors"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// GetPrompt 获取用户输入
func GetPrompt(localizer *i18n.Localizer) (string, error) {
	return askInput(localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "enterPrompt",
	}))
}

// GetAdditionalInfo 获取用户补充信息
func GetAdditionalInfo(localizer *i18n.Localizer) (string, error) {
	return askInput(localizer.MustLocalize(&i18n.LocalizeConfig{
		MessageID: "enterAdditionalInfo",
	}))
}

// SelectOption 展示选项并返回用户选择。
func SelectOption(message string, options []string) (string, error) {
	var selectedOption string
	selectPrompt := &survey.Select{
		Message: message,
		Options: options,
		Default: options[0],
	}

	if err := survey.AskOne(selectPrompt, &selectedOption); err != nil {
		return "", err
	}

	return selectedOption, nil
}

// IsInterrupt 判断错误是否由 Ctrl+C 中断触发。
func IsInterrupt(err error) bool {
	return errors.Is(err, terminal.InterruptErr)
}


func askInput(message string) (string, error) {
	
	var value string
	// 创建输入提示
	prompt := &survey.Input{
		Message: message,
	}

	// 询问用户输入
	if err := survey.AskOne(prompt, &value); err != nil {
		return "", err
	}

	return value, nil
}
