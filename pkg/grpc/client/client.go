package client

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/aleitner/spacialPhone/pkg/user/coordinates"

	call "github.com/aleitner/spacialPhone/internal/protobuf"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type CallClient interface {
	Call(ctx context.Context, audioInput io.Reader) error
	CloseConn() error
}

type Client struct {
	route      call.PhoneClient
	conn       *grpc.ClientConn
	logger     *log.Logger
	id         int
	coordinate coordinates.Coordinate
}

func NewContactConnection(id int, logger *log.Logger, conn *grpc.ClientConn) CallClient {
	return &Client{
		id:     id,
		logger: logger,
		conn:   conn,
		route:  call.NewPhoneClient(conn),
	}
}

func (client *Client) Call(ctx context.Context, audioInput io.Reader) error {
	clientId := strconv.Itoa(client.id)
	md := metadata.Pairs("client-id", clientId)
	ctx = metadata.NewOutgoingContext(ctx, md)

	stream, err := client.route.Call(ctx)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	// Send data
	go func() {
		for {
			buf := make([]byte, 1024*16) // Optimal sending size is 16KiB-64KiB
			_, err := audioInput.Read(buf)
			if err != nil {
				// server returns with nil
				if err != io.EOF {
					client.logger.Errorf("audio read fail: %s/n", err)
				}
				break
			}

			err = stream.Send(&call.CallData{
				AudioData: &call.AudioData{
					AudioData:     buf,
					AudioEncoding: "idk",
					Length:        uint64(len(buf)),
				},
				UserMetaData: &call.UserMetaData{
					Id:          uint64(client.id),
					Coordinates: client.coordinate.ToGRPC(),
				},
			})
			if err != nil {
				// server returns with nil
				if err != io.EOF {
					client.logger.Errorf("stream Send fail: %s/n", err)
				}

				break
			}
		}

		err := stream.CloseSend()
		if err != nil {
			client.logger.Errorf("close send fail: %s\n", err)
		}
		wg.Done()
	}()
	wg.Add(1)

	// Receive data
	go func() {
		for {
			res, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					log.Fatalf("stream Recv fail: %s/n", err)
				}
				break
			}

			userMetaData := res.GetUserMetaData()
			audioData := res.GetAudioData()
			fmt.Printf("%s: %s\n", userMetaData.GetId(), string(audioData.GetAudioData()[:audioData.GetLength()]))
		}

		wg.Done()
	}()
	wg.Add(1)

	wg.Wait()
	return nil
}

// CloseConn closes conn
func (client *Client) CloseConn() error {
	return client.conn.Close()
}
