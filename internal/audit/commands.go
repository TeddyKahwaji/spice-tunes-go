package audit

import (
	"errors"
	"net/http"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
)

type CogConfig struct {
	Session    *discordgo.Session
	Logger     *zap.Logger
	HTTPClient *http.Client
}

type AuditCog struct {
	session    *discordgo.Session
	logger     *zap.Logger
	httpClient *http.Client
}

func NewAuditCog(config *CogConfig) (*AuditCog, error) {
	if config.Logger == nil ||
		config.HTTPClient == nil ||
		config.Session == nil {
		return nil, errors.New("config was populated with nil value")
	}

	auditCog := &AuditCog{
		session:    config.Session,
		httpClient: config.HTTPClient,
		logger:     config.Logger,
	}

	// handles all incoming commands
	auditCog.session.AddHandler(auditCog.commandHandler)

	return auditCog, nil
}
