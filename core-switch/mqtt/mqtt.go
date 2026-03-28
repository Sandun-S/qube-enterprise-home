package mqtt

import (
	"fmt"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

type MQTTCfg struct {
	Enabled bool   `yaml:"Enabled"`
	Host    string `yaml:"Host"`
	Port    int    `yaml:"Port"`
	User    string `yaml:"User"`
	Pass    string `yaml:"Pass"`
}

var conf MQTTCfg
var client mqtt.Client
var log *logrus.Logger
var sendLock sync.Mutex

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
	fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	fmt.Println("Connected to MQTT broker")
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	fmt.Printf("Connection lost: %s", err.Error())
}

func Init(l *logrus.Logger, c MQTTCfg) {

	log = l
	conf = c

	if !conf.Enabled {
		return
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", conf.Host, conf.Port))
	opts.SetClientID("core-switch-client")
	opts.SetUsername(conf.User)
	opts.SetPassword(conf.Pass)
	opts.SetDefaultPublishHandler(messagePubHandler)
	opts.OnConnect = connectHandler
	opts.OnConnectionLost = connectLostHandler

	client = mqtt.NewClient(opts)

	token := client.Connect()
	token.Wait()

	err := token.Error()
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Println("Connected to MQTT server")
}

func Send(topic string, msg string) error {

	sendLock.Lock()
	defer sendLock.Unlock()

	log.Debugf("%s %#v\n", topic, msg)
	token := client.Publish(topic, 1, false, msg)
	token.Wait()

	return token.Error()
}
