// aqui estamos implementando la interfaz de CircuitRepository
package postgres

import (
	"database/sql"
	"fmt"
	"gpon-sync/internal/core"

	_ "github.com/go-sql-driver/mysql" // Driver MySQL implícito
)

type PostgresRepo struct {
	db *sql.DB
}

// NewPostgresRepo: Crea una nueva instancia de PostgresRepo (compatible con MySQL)
func NewPostgresRepo(connStr string) (*PostgresRepo, error) {
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	return &PostgresRepo{db: db}, nil
}

// FetchPendingCircuits: Obtiene TODOS los circuitos sin discriminar valores vacíos
func (r *PostgresRepo) FetchPendingCircuits() ([]core.Circuit, error) {
	// Según requerimiento: obtener TODOS los CID sin filtro
	query := `SELECT CID FROM circuitos`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var circuits []core.Circuit
	for rows.Next() {
		var c core.Circuit
		// Solo escaneamos circuit_id (CID), los demás campos se obtienen después
		if err := rows.Scan(&c.CID); err != nil {
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

	// Actualización de todos los campos según el flujo de trabajo
	// MySQL usa backticks para nombres de columnas y ? para parámetros
	// Nota: VLAN se ignora, no se actualiza
	stmt, err := tx.Prepare(
		"UPDATE circuitos " +
		"SET `RxPower`=?, `StatusGpon`=?, `PPPoEUsername`=?, `PPPoEPassword`=? " +
		"WHERE `CID`=?")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, d := range data {
		_, err := stmt.Exec(d.RxPower, d.StatusGpon, d.PPPoEUsername, d.PPPoEPassword, d.CircuitID)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("error actualizando circuito %s: %v", d.CircuitID, err)
		}
	}
	return tx.Commit()
}
