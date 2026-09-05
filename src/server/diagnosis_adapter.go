package server

import (
	"database/sql"

	"MyOfferPilot/src/server/diagnosis"
)

type DiagnosisRecord = diagnosis.Record
type SM2State = diagnosis.SM2State
type diagnosisStore = diagnosis.Store
type DiagnosisService = diagnosis.Service
type DiagnosisRepository = diagnosis.Repository
type DiagnosisProjector = diagnosis.Projector
type DiagnosisProjectorFunc = diagnosis.ProjectorFunc

func newDiagnosisStore() *diagnosis.Store {
	return diagnosis.NewStore()
}

func NewDiagnosisService(store *diagnosis.Store, repository diagnosis.Repository, projector diagnosis.Projector) *diagnosis.Service {
	return diagnosis.NewService(store, repository, projector)
}

func newMySQLDiagnosisRepository(db *sql.DB) diagnosis.Repository {
	return diagnosis.NewMySQLRepository(db)
}

func diagnosisScope(userID, sessionID string) string {
	return diagnosis.Scope(userID, sessionID)
}

func sm2Key(scope, dimension string) string {
	return diagnosis.SM2Key(scope, dimension)
}
