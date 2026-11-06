package captcha

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"github.com/go-gomail/gomail"
	"github.com/go-kratos/kratos/v2/log"
	"math/big"
	"time"
	"vw_gateway/internal/conf"
)

const ExpirationTime = 5 * time.Minute

type EmailOpt func(*Email)

type Email struct {
	host       string
	port       int
	username   string
	password   string
	servername string
	from       string
	subject    string
	htmlFormat string
	logger     *log.Helper
}

func NewEmail(cfg *conf.Email, logger log.Logger) *Email {
	return &Email{
		host:       cfg.SmtpHost,
		port:       int(cfg.SmtpPort),
		username:   cfg.SmtpUsername,
		password:   cfg.SmtpPassword,
		servername: cfg.SmtpServername,
		from:       "Zey " + "<" + cfg.SmtpUsername + ">",
		subject:    "验证码",
		htmlFormat: fmt.Sprintf("您的验证码为： <b> %%s </b>"),
		logger:     log.NewHelper(logger),
	}
}

// SendCode 发送验证码
func (ecfg *Email) SendCode(ctx context.Context, to, code string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", ecfg.from)
	m.SetHeader("To", to)
	m.SetHeader("Subject", ecfg.subject)
	m.SetBody("text/html", fmt.Sprintf(ecfg.htmlFormat, code))
	d := gomail.NewDialer(ecfg.host, ecfg.port, ecfg.username, ecfg.password)
	d.TLSConfig = &tls.Config{InsecureSkipVerify: false, ServerName: ecfg.servername}
	err := d.DialAndSend(m)
	if err != nil {
		ecfg.logger.Error(err)
	}
	return err
}

// CreateVerificationCode 生成6位验证码
func (ecfg *Email) CreateVerificationCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (ecfg *Email) NewWithOption(options ...EmailOpt) *Email {
	e := &Email{
		host:       ecfg.host,
		port:       ecfg.port,
		username:   ecfg.username,
		password:   ecfg.password,
		servername: ecfg.servername,
		from:       ecfg.from,
		subject:    ecfg.subject,
		htmlFormat: ecfg.htmlFormat,
		logger:     ecfg.logger,
	}
	for _, o := range options {
		o(e)
	}
	return e
}
func (ecfg *Email) SetHTML(html string) EmailOpt {
	return func(e *Email) {
		e.htmlFormat = html
	}
}

func (ecfg *Email) SetSubject(subject string) EmailOpt {
	return func(e *Email) {
		e.subject = subject
	}
}

func (ecfg *Email) SetFrom(from string) EmailOpt {
	return func(e *Email) {
		e.from = from
	}
}
func (ecfg *Email) SetHost(host string) EmailOpt {
	return func(e *Email) {
		e.host = host
	}
}

func (ecfg *Email) SetPort(port int) EmailOpt {
	return func(e *Email) {
		e.port = port
	}
}

func (ecfg *Email) SetUsername(username string) EmailOpt {
	return func(e *Email) {
		e.username = username
	}
}

func (ecfg *Email) SetPassword(password string) EmailOpt {
	return func(e *Email) {
		e.password = password
	}
}

func (ecfg *Email) SetServername(servername string) EmailOpt {
	return func(e *Email) {
		e.servername = servername
	}
}
