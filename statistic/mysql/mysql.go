package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/p4gefau1t/trojan-go/config"

	// MySQL Driver
	_ "github.com/go-sql-driver/mysql"

	"github.com/p4gefau1t/trojan-go/common"
	"github.com/p4gefau1t/trojan-go/log"
	"github.com/p4gefau1t/trojan-go/statistic"
	"github.com/p4gefau1t/trojan-go/statistic/memory"
)

const Name = "MYSQL"

type Authenticator struct {
	*memory.Authenticator
	db             *sql.DB
	updateDuration time.Duration
	ctx            context.Context
}

func (a *Authenticator) updater() {
	for {
		for _, user := range a.ListUsers() {
			//swap upload and download for users
			hash := user.Hash()
			sent, recv := user.GetAndResetTraffic()

			s, err := a.db.Exec("UPDATE `users` SET `upload`=`upload`+?, `download`=`download`+? WHERE `password`=?;", recv, sent, hash)
			if err != nil {
				log.Error(common.NewError("failed to update data to user table").Base(err))
				continue
			}
			if r, err := s.RowsAffected(); err != nil {
				if r == 0 {
					a.DelUser(hash)
				}
			}
			/* GetIP := user.GetIP()
			log.Infof("IP链接数：" + strconv.Itoa(GetIP)) */
			//update node status
			cfg := config.FromContext(a.ctx, Name).(*Config)
			nodeid := cfg.MySQL.NodeID
			if nodeid == 0 {
				log.Error(common.NewError("node_id configuration error").Base(err))
			}
			u, err := a.db.Exec("UPDATE `ss_node` SET `node_heartbeat`=?,`node_bandwidth`=`node_bandwidth`+?+? WHERE `id`=?", time.Now().Unix(), recv, sent, nodeid)
			if err != nil {
				log.Error(common.NewError("failed to update data to ss_node table").Base(err))
				continue
			}
			if n, err := u.RowsAffected(); err != nil {
				if n == 0 {
					log.Error(common.NewError("failed to update data to node_id " + strconv.Itoa(nodeid)).Base(err))
				}
			}
			//update node traffic
		}
		log.Info("buffered data has been written into the database")

		//update memory
		rows, err := a.db.Query("SELECT password,quota,download,upload FROM users")
		if err != nil {
			log.Error(common.NewError("failed to pull data from the database").Base(err))
			time.Sleep(a.updateDuration)
			continue
		}
		for rows.Next() {
			var hash string
			var quota, download, upload int64
			err := rows.Scan(&hash, &quota, &download, &upload)
			if err != nil {
				log.Error(common.NewError("failed to obtain data from the query result").Base(err))
				break
			}
			if download+upload < quota || quota < 0 {
				a.AddUser(hash)
			} else {
				a.DelUser(hash)
			}
		}

		select {
		case <-time.After(a.updateDuration):
		case <-a.ctx.Done():
			log.Debug("MySQL daemon exiting...")
			return
		}
	}
}

func connectDatabase(driverName, username, password, ip string, port int, dbName string) (*sql.DB, error) {
	path := strings.Join([]string{username, ":", password, "@tcp(", ip, ":", fmt.Sprintf("%d", port), ")/", dbName, "?charset=utf8"}, "")
	return sql.Open(driverName, path)
}

func NewAuthenticator(ctx context.Context) (statistic.Authenticator, error) {
	cfg := config.FromContext(ctx, Name).(*Config)
	db, err := connectDatabase(
		"mysql",
		cfg.MySQL.Username,
		cfg.MySQL.Password,
		cfg.MySQL.ServerHost,
		cfg.MySQL.ServerPort,
		cfg.MySQL.Database,
	)
	if err != nil {
		return nil, common.NewError("Failed to connect to database server").Base(err)
	}
	memoryAuth, err := memory.NewAuthenticator(ctx)
	if err != nil {
		return nil, err
	}
	a := &Authenticator{
		db:             db,
		ctx:            ctx,
		updateDuration: time.Duration(cfg.MySQL.CheckRate) * time.Second,
		Authenticator:  memoryAuth.(*memory.Authenticator),
	}
	go a.updater()
	log.Debug("mysql authenticator created")
	return a, nil
}

func init() {
	statistic.RegisterAuthenticatorCreator(Name, NewAuthenticator)
}
