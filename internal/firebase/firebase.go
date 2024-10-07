package firebase

import (
	"context"
	"fmt"

	fs "cloud.google.com/go/firestore"
)

type Client struct {
	firestoreClient *fs.Client
}

func NewClient(firestoreClient *fs.Client) *Client {
	return &Client{
		firestoreClient: firestoreClient,
	}
}

func (c *Client) Close() error {
	return c.firestoreClient.Close()
}

func (c *Client) GetDocumentFromCollection(ctx context.Context, collection string, document string) *fs.DocumentRef {
	return c.firestoreClient.Collection(collection).Doc(document)
}

func (c *Client) CreateDocument(ctx context.Context, collection string, document string, data interface{}) error {
	_, err := c.firestoreClient.Collection(collection).Doc(document).Create(ctx, data)

	return err
}

func (c *Client) DeleteDocument(ctx context.Context, collection string, document string) error {
	_, err := c.firestoreClient.Collection(collection).Doc(document).Delete(ctx)
	if err != nil {
		return fmt.Errorf("error deleting document from collection: %w", err)
	}

	return err
}

func (c *Client) UpdateDocument(ctx context.Context, collection string, document string, data map[string]interface{}) error {
	updates := []fs.Update{}

	for key, value := range data {
		updates = append(updates, fs.Update{
			Path:  key,
			Value: value,
		})
	}

	if _, err := c.firestoreClient.Collection(collection).Doc(document).Update(ctx, updates); err != nil {
		return fmt.Errorf("error updating document: %w", err)
	}

	return nil
}
