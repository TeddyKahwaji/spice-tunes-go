package firebase

import (
	"context"
	"fmt"

	fs "cloud.google.com/go/firestore"
	"go.uber.org/zap"
)

type client struct {
	firestoreClient    *fs.Client
	cloudStorageClient *gs.Client
	logger             *zap.Logger
}

func NewClient(firestoreClient *fs.Client, storageClient *gs.Client, logger *zap.Logger) *client {
	return &client{
		firestoreClient:    firestoreClient,
		cloudStorageClient: storageClient,
		logger:             logger,
	}
}

func (f *FirebaseAdapter) GetDocumentFromCollection(ctx context.Context, collection string, document string) (map[string]interface{}, error) {
	fs, err := f.firestoreClient.Collection(collection).Doc(document).Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting document from collection %w", err)
	}

	data := fs.Data()

	return data, nil
}

func (f *FirebaseAdapter) CreateDocument(ctx context.Context, collection string, document string, data interface{}) error {
	_, err := f.firestoreClient.Collection(collection).Doc(document).Create(ctx, data)

	return err
}

func (f *FirebaseAdapter) DeleteDocument(ctx context.Context, collection string, document string) error {
	_, err := f.firestoreClient.Collection(collection).Doc(document).Delete(ctx)
	if err != nil {
		return fmt.Errorf("error deleting document from collection: %w", err)
	}

	return err
}

func (f *FirebaseAdapter) UpdateDocument(ctx context.Context, collection string, document string, data map[string]interface{}) error {
	updates := []fs.Update{}

	for key, value := range data {
		updates = append(updates, fs.Update{
			Path:  key,
			Value: value,
		})
	}

	if _, err := f.firestoreClient.Collection(collection).Doc(document).Update(ctx, updates); err != nil {
		return fmt.Errorf("error updating document: %w", err)
	}

	return nil
}
