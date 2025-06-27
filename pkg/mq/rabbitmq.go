package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/streadway/amqp"
	"github.com/yourusername/hypgo/pkg/config"
	"github.com/yourusername/hypgo/pkg/logger"
)
