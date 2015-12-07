package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/applikatoni/applikatoni/deploy"
	"github.com/applikatoni/applikatoni/models"

	"log"
	"net/http"
	"text/template"
)

const slackSummaryTmplStr = `{{.GitHubRepo}} {{if .Success}}Successfully Deployed{{else}}Deploy Failed{{end}}:
{{.Username}} deployed {{.Branch}} on {{.Target}} :pizza:

> {{.Comment}}
<{{.GitHubUrl}}|View latest commit on GitHub>
<{{.DeploymentURL}}|Open deployment in Applikatoni>`

var slackSummaryTemplate = template.Must(template.New("slackSummary").Parse(slackSummaryTmplStr))

type slackMsg struct {
	Text string `json:"text"`
}

func NotifySlack(db *sql.DB, deploymentId int, success bool) {
	deployment, err := getDeployment(db, deploymentId)
	if err != nil {
		log.Printf("Could not find deployment with id %v, %s\n", deploymentId, err)
		return
	}

	application, err := findApplication(deployment.ApplicationName)
	if err != nil {
		log.Printf("Could not find application with name %v, %s\n", deployment.ApplicationName, err)
		return
	}

	target, err := findTarget(application, deployment.TargetName)
	if err != nil {
		log.Printf("Could not find target with name %v, %s\n", deployment.TargetName, err)
		return
	}

	if target.SlackUrl == "" {
		return
	}

	user, err := getUser(db, deployment.UserId)
	if err != nil {
		log.Printf("Could not find User with id %v, %s\n", deployment.UserId, err)
		return
	}

	summary, err := generateSlackSummary(deployment, application, user, success)

	if err != nil {
		log.Printf("Could not generate Slack deployment summary, %s\n", err)
		return
	}

	SendSlackRequest(deployment, target, application, summary)
}

func generateSlackSummary(d *models.Deployment, a *models.Application, u *models.User, success bool) (string, error) {
	var summary bytes.Buffer

	var protocol string
	if config.SSLEnabled {
		protocol = "https"
	} else {
		protocol = "http"
	}

	deploymentUrl := fmt.Sprintf("%s://%s/%v/deployments/%v",
		protocol,
		config.Host,
		a.GitHubRepo, d.Id)

	gitHubUrl := fmt.Sprintf("https://github.com/%v/%v/commit/%v",
		a.GitHubOwner, a.GitHubRepo, d.CommitSha)

	err := slackSummaryTemplate.Execute(&summary, map[string]interface{}{
		"GitHubRepo":    a.GitHubRepo,
		"Success":       success,
		"Branch":        d.Branch,
		"Target":        d.TargetName,
		"Username":      u.Name,
		"Comment":       d.Comment,
		"GitHubUrl":     gitHubUrl,
		"DeploymentURL": deploymentUrl,
	})

	return summary.String(), err
}

func SendSlackRequest(d *models.Deployment, t *models.Target, a *models.Application, summary string) {
	payload, err := json.Marshal(slackMsg{Text: summary})

	if err != nil {
		log.Printf("Error creating Slack notification %s\n", err)
		return
	}

	resp, err := http.Post(t.SlackUrl, "application/json", bytes.NewBuffer(payload))

	if err != nil || resp.StatusCode != 200 {
		log.Printf("Error while notifying Slack about deployment of %v on %v, %v! err: %s, resp: %s\n",
			d.ApplicationName,
			d.TargetName,
			d.CommitSha,
			err,
			resp.Status)
		return
	}

	log.Printf("Successfully notified Slack about deployment of %v on %v, %v!\n", d.ApplicationName, d.TargetName, d.CommitSha)
}

func newSlackNotifier(db *sql.DB) deploy.Listener {
	fn := func(logs <-chan deploy.LogEntry) {
		for entry := range logs {
			if entry.EntryType == deploy.DEPLOYMENT_SUCCESS {
				go NotifySlack(db, entry.DeploymentId, true)
			} else if entry.EntryType == deploy.DEPLOYMENT_FAIL {
				go NotifySlack(db, entry.DeploymentId, false)
			}
		}
	}

	return fn
}