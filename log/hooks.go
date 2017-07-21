package log

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"text/template"

	"github.com/sirupsen/logrus"
)

// MailHook is for hooking Panic in web application
type MailHook struct {
	LevelNames []string
	Mail
}

// Fire forwards an error to MailHook
func (hook *MailHook) Fire(entry *logrus.Entry) error {
	addr := strings.Split(hook.Mail.Addr, ":")
	if len(addr) != 2 {
		return errors.New("Invalid Mail Address")
	}
	host := addr[0]
	subject := fmt.Sprintf("[%s] %s: %s", entry.Level, host, entry.Message)

	html := `
        {{.Message}}

        {{range $key, $value := .Data}}
        {{$key}}: {{$value}}
        {{end}}
        `
	b := bytes.NewBuffer(make([]byte, 0))
	t := template.Must(template.New("mail body").Parse(html))
	if err := t.Execute(b, entry); err != nil {
		return err
	}
	body := fmt.Sprintf("%s", b)

	return hook.Mail.SendMail(subject, body)
}

// Levels contains hook levels to be catched
func (hook *MailHook) Levels() []logrus.Level {
	levels := []logrus.Level{}
	for _, v := range hook.LevelNames {
		lv, _ := logrus.ParseLevel(v)
		levels = append(levels, lv)
	}
	return levels
}

// StderrHook is for hooking Panic in web application
type StderrHook struct {
	LevelNames []string
}

func (hook *StderrHook) Levels() []logrus.Level {
	levels := []logrus.Level{}
	for _, v := range hook.LevelNames {
		lv, _ := logrus.ParseLevel(v)
		levels = append(levels, lv)
	}
	return levels
}

func (hook *StderrHook) Fire(entry *logrus.Entry) error {
	thisLog := reflect.ValueOf(*entry.Logger).Interface().(logrus.Logger)
	entry.Logger = &thisLog
	entry.Logger.Out = os.Stderr
	return nil
}
