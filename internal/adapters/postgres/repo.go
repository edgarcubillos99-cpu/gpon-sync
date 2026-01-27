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
	// 🚧 NECESITO INFO: Nombre real de la tabla y columnas
	query := `SELECT id, circuit_code, olt_host FROM circuits_table WHERE status = 'active'`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var circuits []core.Circuit
	for rows.Next() {
		var c core.Circuit
		if err := rows.Scan(&c.ID, &c.CircuitID, &c.OLT_Hostname); err != nil {
			return nil, err
		}
		circuits = append(circuits, c)
	}
	return circuits, nil
}

// UpdateCircuitBatch: Actualiza un batch de circuitos en la base de datos
func (r *PostgresRepo) UpdateCircuitBatch(data []core.EnrichedData) error {
	// Implementación básica. Para producción masiva, usar COPY o transacciones por bloques.
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}

	// 🚧 NECESITO INFO: Nombres de columnas destino
	stmt, err := tx.Prepare(`
		UPDATE circuits_table
		SET vlan=$1, pppoe_user=$2, pppoe_pass=$3, gpon_status=$4, rx_power=$5, last_updated=NOW()
		WHERE circuit_code=$6
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, d := range data {
		if _, err := stmt.Exec(d.VLAN, d.PPPoEUser, d.PPPoEPass, d.StatusGpon, d.RxPower, d.CircuitID); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
