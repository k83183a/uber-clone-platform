package kafka

import (
    "encoding/json"
    "log"
    "time"

    "github.com/IBM/sarama"
)

type Producer struct {
    producer sarama.SyncProducer
}

func NewProducer(brokers string) (*Producer, error) {
    config := sarama.NewConfig()
    config.Producer.RequiredAcks = sarama.WaitForAll
    config.Producer.Retry.Max = 5
    config.Producer.Return.Successes = true
    producer, err := sarama.NewSyncProducer([]string{brokers}, config)
    if err != nil {
        return nil, err
    }
    return &Producer{producer: producer}, nil
}

func (p *Producer) PublishUserCreated(userID, email, role string) {
    event := map[string]interface{}{
        "event_type": "user.created",
        "user_id":    userID,
        "email":      email,
        "role":       role,
        "timestamp":  time.Now().Unix(),
    }
    data, _ := json.Marshal(event)
    msg := &sarama.ProducerMessage{
        Topic: "user.created",
        Value: sarama.ByteEncoder(data),
    }
    _, _, err := p.producer.SendMessage(msg)
    if err != nil {
        log.Printf("Failed to publish user.created: %v", err)
    }
}

func (p *Producer) Close() {
    p.producer.Close()
}