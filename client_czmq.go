// +build goczmq

package boomer

import (
	"fmt"
	"github.com/zeromq/goczmq"
	"log"
	"time"
)

type czmqSocketClient struct {
	masterHost string
	masterPort int
	identity   string

	dealerSocket *goczmq.Sock

	fromMaster             chan *message
	toMaster               chan *message
	disconnectedFromMaster chan bool
	shutdownChan           chan bool
}

func newClient(masterHost string, masterPort int, identity string) (client *czmqSocketClient) {
	log.Println("Boomer is built with goczmq support.")
	client = &czmqSocketClient{
		masterHost:             masterHost,
		masterPort:             masterPort,
		identity:               identity,
		fromMaster:             make(chan *message, 100),
		toMaster:               make(chan *message, 100),
		disconnectedFromMaster: make(chan bool),
		shutdownChan:           make(chan bool),
	}

	return client
}

func (c *czmqSocketClient) connect() (err error) {
	addr := fmt.Sprintf("tcp://%s:%d", c.masterHost, c.masterPort)
	dealer := goczmq.NewSock(goczmq.Dealer)
	dealer.SetOption(goczmq.SockSetIdentity(c.identity))
	err = dealer.Connect(addr)
	if err != nil {
		return err
	}

	c.dealerSocket = dealer

	log.Printf("Boomer is connected to master(%s) press Ctrl+c to quit.\n", addr)

	go c.recv()
	go c.send()

	return nil
}

func (c *czmqSocketClient) close() {
	close(c.shutdownChan)
	c.dealerSocket.Destroy()
}

func (c *czmqSocketClient) recvChannel() chan *message {
	return c.fromMaster
}

func (c *czmqSocketClient) recv() {
	for {
		select {
		case <-c.shutdownChan:
			return
		default:
			msg, _, err := c.dealerSocket.RecvFrame()
			if err != nil {
				log.Printf("Error reading: %v\n", err)
				continue
			}
			decodedMsg, err := newMessageFromBytes(msg)
			if err != nil {
				log.Printf("Msgpack decode fail: %v\n", err)
				continue
			}
			if decodedMsg.NodeID != c.identity {
				log.Printf("Recv a %s message for node(%s), not for me(%s), dropped.\n", decodedMsg.Type, decodedMsg.NodeID, c.identity)
				continue
			}
			c.fromMaster <- decodedMsg
		}
	}
}

func (c *czmqSocketClient) sendChannel() chan *message {
	return c.toMaster
}

func (c *czmqSocketClient) send() {
	for {
		select {
		case <-c.shutdownChan:
			return
		case msg := <-c.toMaster:
			c.sendMessage(msg)
			if msg.Type == "quit" {
				c.disconnectedFromMaster <- true
			}
		}
	}
}

func (c *czmqSocketClient) sendMessage(msg *message) {
	serializedMessage, err := msg.serialize()
	if err != nil {
		log.Printf("Msgpack encode fail: %v\n", err)
		return
	}
	retries :=0

	for {
		err = c.dealerSocket.SendFrame(serializedMessage, goczmq.FlagNone)
		if err != nil {
			retries++
			if retries > 3 {
				log.Printf("Error sending after #{retries} retries #{err}\n")
				break
			}
			time.Sleep(time.Millisecond * 5)
			continue
		}
		break
	}
	if retries > 0 && err == nil{
		log.Printf("sendMessage succeeded after #{retries} retries\n")
	}
}

func (c *czmqSocketClient) disconnectedChannel() chan bool {
	return c.disconnectedFromMaster
}
