package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"community-waste/internal/domain"
)

type ConfirmationRepository interface {
	Reference(context.Context, string) (domain.PaymentReference, error)
	WithinTransaction(context.Context, func(ConfirmationTx) error) error
}

type ConfirmationTx interface {
	LockHousehold(context.Context, string) error
	PickupForUpdate(context.Context, string) (domain.Pickup, error)
	PaymentByIDForUpdate(context.Context, string) (domain.Payment, error)
	MarkPaid(context.Context, string, string) (domain.Payment, error)
}

type ProofStore interface {
	Save(context.Context, []byte, string) (string, error)
	Delete(string) error
}

type Confirmation struct {
	repository ConfirmationRepository
	store      ProofStore
}

func NewConfirmation(repository ConfirmationRepository, store ProofStore) *Confirmation {
	return &Confirmation{repository: repository, store: store}
}

func (s *Confirmation) Confirm(ctx context.Context, id string, content []byte) (domain.Payment, error) {
	if !validUUID(id) {
		return domain.Payment{}, domain.ErrInvalidID
	}
	extension, err := proofExtension(content)
	if err != nil {
		return domain.Payment{}, err
	}
	reference, err := s.repository.Reference(ctx, id)
	if err != nil {
		return domain.Payment{}, err
	}
	var payment domain.Payment
	var path string
	err = s.repository.WithinTransaction(ctx, func(tx ConfirmationTx) error {
		if err := tx.LockHousehold(ctx, reference.HouseholdID); err != nil {
			return err
		}
		pickup, err := tx.PickupForUpdate(ctx, reference.WasteID)
		if err != nil {
			return err
		}
		payment, err = tx.PaymentByIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if payment.Status != domain.PaymentPending {
			return domain.ErrPaymentNotPending
		}
		if pickup.Status == domain.PickupCanceled {
			return domain.ErrInvalidTransition
		}
		path, err = s.store.Save(ctx, content, extension)
		if err != nil {
			return err
		}
		payment, err = tx.MarkPaid(ctx, id, path)
		return err
	})
	if err != nil {
		if path != "" && !errors.Is(err, domain.ErrCommitUncertain) {
			err = errors.Join(err, s.store.Delete(path))
		}
		if path != "" && errors.Is(err, domain.ErrCommitUncertain) {
			err = fmt.Errorf("%w; proof retained at %s", err, path)
		}
		return domain.Payment{}, err
	}
	return payment, nil
}

func proofExtension(content []byte) (string, error) {
	if len(content) == 0 {
		return "", &domain.ValidationError{Fields: map[string]string{"proof_file": "Proof file is required"}}
	}
	if len(content) > domain.MaxProofSize {
		return "", domain.ErrProofTooLarge
	}
	switch http.DetectContentType(content) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "application/pdf":
		return ".pdf", nil
	default:
		return "", domain.ErrUnsupportedProof
	}
}
