// aqui estamos implementando la interfaz de CircuitRepository
package postgres

import (
	"database/sql"
	"gpon-sync/internal/core"

	_ "github.com/lib/pq" // Driver Postgres implícito
)

type PostgresRepo struct {
	db *sql.DB
}

// NewPostgresRepo: Crea una nueva instancia de PostgresRepo
func NewPostgresRepo(connStr string) (*PostgresRepo, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	return &PostgresRepo{db: db}, nil
}

// FetchPendingCircuits: Obtiene los circuitos pendientes de sincronización
func (r *PostgresRepo) FetchPendingCircuits() ([]core.Circuit, error) {
	// 🚧 'circuit_id' es el CID en la DB
	query := `SELECT circuit_id FROM servicios WHERE StatusGpon IS NULL` // Ejemplo de filtro

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var circuits []core.Circuit
	for rows.Next() {
		var c core.Circuit
		if err := rows.Scan(&c.ID, &c.CID, &c.OLT_Hostname); err != nil {
			return nil, err
		}
		circuits = append(circuits, c)
	}
	return circuits, nil
}

// UpdateCircuitBatch: Actualiza un batch de circuitos en la base de datos
func (r *PostgresRepo) UpdateCircuitBatch(data []core.EnrichedData) error {
	if len(data) == 0 {
		return nil
	}

	// Implementación básica. Para producción masiva, usar COPY o transacciones por bloques.
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	// 🚧 CAMBIO: Columnas exactas RxPower y StatusGpon
	stmt, err := tx.Prepare(`
		UPDATE servicios 
        SET "RxPower"=$1, "StatusGpon"=$2, "VLAN"=$3, "PPPoEUser"=$4, "PPPoEPass"=$5 
        WHERE circuit_id=$6`)

	for _, d := range data {
		stmt.Exec(d.RxPower, d.StatusGpon, d.CircuitID)
	}
	return tx.Commit()
}
