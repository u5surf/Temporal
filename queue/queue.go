package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/RTradeLtd/config"
	"github.com/RTradeLtd/database"
	"github.com/streadway/amqp"
)

func (qm *Manager) setupLogging() error {
	logFileName := fmt.Sprintf("/var/log/temporal/%s_service.log", qm.QueueName)
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0640)
	if err != nil {
		return err
	}
	logger := log.New()
	logger.Out = logFile
	qm.logger = logger
	qm.logger.Info("Logging initialized")
	return nil
}

func (qm *Manager) parseQueueName(queueName string) error {
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	qm.QueueName = fmt.Sprintf("%s+%s", host, queueName)
	return nil
}

// Initialize is used to connect to the given queue, for publishing or consuming purposes
func Initialize(queueName, connectionURL string, publish, service bool, logFilePath ...string) (*Manager, error) {
	conn, err := setupConnection(connectionURL)
	if err != nil {
		return nil, err
	}
	qm := Manager{connection: conn}
	if err := qm.OpenChannel(); err != nil {
		return nil, err
	}

	qm.QueueName = queueName
	qm.Service = queueName
	if service {
		if err = qm.setupLogging(); err != nil {
			return nil, err
		}
	}

	// Declare Non Default exchanges for the particular queue
	switch queueName {
	case IpfsPinQueue:
		if err = qm.parseQueueName(queueName); err != nil {
			return nil, err
		}
		if err = qm.DeclareIPFSPinExchange(); err != nil {
			return nil, err
		}
		qm.ExchangeName = PinExchange
	case IpfsKeyCreationQueue:
		if err = qm.parseQueueName(queueName); err != nil {
			return nil, err
		}
		if err = qm.DeclareIPFSKeyExchange(); err != nil {
			return nil, err
		}
		qm.ExchangeName = IpfsKeyExchange
	}
	// we only need to declare a queue if we're consuming (aka, service)
	if publish {
		return &qm, nil
	}
	if err := qm.DeclareQueue(); err != nil {
		return nil, err
	}
	return &qm, nil
}

func setupConnection(connectionURL string) (*amqp.Connection, error) {
	conn, err := amqp.Dial(connectionURL)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// OpenChannel is used to open a channel to the rabbitmq server
func (qm *Manager) OpenChannel() error {
	ch, err := qm.connection.Channel()
	if err != nil {
		return err
	}
	if qm.logger != nil {
		qm.LogInfo("channel opened")
	}
	qm.channel = ch
	return qm.channel.Qos(10, 0, false)
}

// DeclareQueue is used to declare a queue for which messages will be sent to
func (qm *Manager) DeclareQueue() error {
	// we declare the queue as durable so that even if rabbitmq server stops
	// our messages won't be lost
	q, err := qm.channel.QueueDeclare(
		qm.QueueName, // name
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return err
	}
	if qm.logger != nil {
		qm.LogInfo("queue declared")
	}
	qm.queue = &q
	return nil
}

// ConsumeMessages is used to consume messages that are sent to the queue
// Question, do we really want to ack messages that fail to be processed?
// Perhaps the error was temporary, and we allow it to be retried?
func (qm *Manager) ConsumeMessages(ctx context.Context, wg *sync.WaitGroup, consumer, dbPass, dbURL, dbUser string, cfg *config.TemporalConfig) error {
	db, err := database.OpenDBConnection(database.DBOptions{
		User:           cfg.Database.Username,
		Password:       cfg.Database.Password,
		Address:        cfg.Database.URL,
		Port:           cfg.Database.Port,
		SSLModeDisable: true,
	})
	if err != nil {
		return err
	}
	// embed database into queue manager
	qm.db = db
	// embed config into queue manager
	qm.cfg = cfg
	// if we are using an exchange, we form a relationship between a queue and an exchange
	// this process is known as binding, and allows consumers to receive messages sent to an exchange
	// We are primarily doing this to allow for multiple consumers, to receive the same message
	// For example using the IpfsKeyExchange, this will setup message distribution such that
	// a single key creation request, will be sent to all of our consumers ensuring that all of our nodes
	// will have the same key in their keystore
	switch qm.ExchangeName {
	case PinRemovalExchange, PinExchange, IpfsKeyExchange:
		if err = qm.channel.QueueBind(
			qm.QueueName,    // name of the queue
			"",              // routing key
			qm.ExchangeName, // exchange
			false,           // noWait
			nil,             // arguments
		); err != nil {
			return err
		}
	default:
		break
	}

	// we do not auto-ack, as if a consumer dies we don't want the message to be lost
	msgs, err := qm.channel.Consume(
		qm.QueueName, // queue
		consumer,     // consumer
		false,        // auto-ack
		false,        // exclusive
		false,        // no-local
		false,        // no-wait
		nil,          // args
	)
	if err != nil {
		return err
	}

	// check the queue name
	switch qm.Service {
	// only parse database file requests
	case DatabaseFileAddQueue:
		return qm.ProcessDatabaseFileAdds(ctx, wg, msgs)
	case IpfsPinQueue:
		return qm.ProccessIPFSPins(ctx, wg, msgs)
	case IpfsFileQueue:
		return qm.ProccessIPFSFiles(ctx, wg, msgs)
	case EmailSendQueue:
		return qm.ProcessMailSends(ctx, wg, msgs)
	case IpnsEntryQueue:
		return qm.ProcessIPNSEntryCreationRequests(ctx, wg, msgs)
	case IpfsKeyCreationQueue:
		return qm.ProcessIPFSKeyCreation(ctx, wg, msgs)
	case IpfsClusterPinQueue:
		return qm.ProcessIPFSClusterPins(ctx, wg, msgs)
	default:
		return errors.New("invalid queue name")
	}
}

//PublishMessageWithExchange is used to publish a message to a given exchange
func (qm *Manager) PublishMessageWithExchange(body interface{}, exchangeName string) error {
	switch exchangeName {
	case PinExchange, PinRemovalExchange, IpfsKeyExchange:
		break
	default:
		return errors.New("invalid exchange name provided")
	}
	bodyMarshaled, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if err = qm.channel.Publish(
		exchangeName, // exchange - this determines which exchange will receive the message
		"",           // routing key
		false,        // mandatory
		false,        // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent, // messages will persist through crashes, etc..
			ContentType:  "text/plain",
			Body:         bodyMarshaled,
		},
	); err != nil {
		return err
	}
	return nil
}

// PublishMessage is used to produce messages that are sent to the queue, with a worker queue (one consumer)
func (qm *Manager) PublishMessage(body interface{}) error {
	bodyMarshaled, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if err = qm.channel.Publish(
		"",            // exchange - this is left empty, and becomes the default exchange
		qm.queue.Name, // routing key
		false,         // mandatory
		false,         // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent, // messages will persist through crashes, etc..
			ContentType:  "text/plain",
			Body:         bodyMarshaled,
		},
	); err != nil {
		return err
	}
	return nil
}

// Close is used to close our queue resources
func (qm *Manager) Close() {
	if err := qm.channel.Close(); err != nil {
		qm.LogError(err, "failed to properly close channel")
	} else {
		qm.LogInfo("properly shutdown channel")
	}
	if err := qm.connection.Close(); err != nil {
		qm.LogError(err, "failed to properly close connection")
	} else {
		qm.LogInfo("properly shutdown connnetion")
	}
}
