package discovery

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/pion/ion/log"
	"go.etcd.io/etcd/clientv3"
)

const (
	defaultDialTimeout      = time.Second * 5
	defaultGrantTimeout     = 5
	defaultOperationTimeout = time.Second * 5
)

type WatchCallback func(clientv3.WatchChan)

type Etcd struct {
	client        *clientv3.Client
	liveKeyID     map[string]clientv3.LeaseID
	liveKeyIDLock sync.RWMutex
}

func newEtcd(endpoints []string) (*Etcd, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: defaultDialTimeout,
	})

	if err != nil {
		log.Errorf("newEtcd err=%v", err)
		return nil, err
	}

	return &Etcd{
		client:    cli,
		liveKeyID: make(map[string]clientv3.LeaseID),
	}, nil
}

func (e *Etcd) keep(key, value string) error {
	resp, err := e.client.Grant(context.TODO(), defaultGrantTimeout)
	if err != nil {
		log.Errorf("Etcd.keep Grant %s %v", key, err)
		return err
	}
	_, err = e.client.Put(context.TODO(), key, value, clientv3.WithLease(resp.ID))
	if err != nil {
		log.Errorf("Etcd.keep Put %s %v", key, err)
		return err
	}

	_, err = e.client.KeepAlive(context.TODO(), resp.ID)
	if err != nil {
		log.Errorf("Etcd.keep %s %v", key, err)
		return err
	}
	e.liveKeyIDLock.Lock()
	e.liveKeyID[key] = resp.ID
	e.liveKeyIDLock.Unlock()
	log.Infof("Etcd.keep %s %v %v", key, value, err)
	return nil
}

func (e *Etcd) del(key string) error {
	e.liveKeyIDLock.Lock()
	delete(e.liveKeyID, key)
	e.liveKeyIDLock.Unlock()
	_, err := e.client.Delete(context.TODO(), key)
	return err
}

func (e *Etcd) watch(key string, watchFunc WatchCallback, prefix bool) error {
	if watchFunc == nil {
		return errors.New("watchFunc is nil")
	}
	if prefix {
		watchFunc(e.client.Watch(context.Background(), key, clientv3.WithPrefix()))
	} else {
		watchFunc(e.client.Watch(context.Background(), key))
	}

	return nil
}

func (e *Etcd) close() error {
	e.liveKeyIDLock.Lock()
	for k, _ := range e.liveKeyID {
		e.client.Delete(context.TODO(), k)
	}
	e.liveKeyIDLock.Unlock()
	return e.client.Close()
}

// func (e *Etcd) Put(key, value string) error { ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
// _, err := e.client.Put(ctx, key, value)
// cancel()

// return err
// }

func (e *Etcd) get(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	resp, err := e.client.Get(ctx, key)
	if err != nil {
		cancel()
		return "", err
	}
	var val string
	for _, ev := range resp.Kvs {
		val = string(ev.Value)
	}
	cancel()

	return val, err
}

func (e *Etcd) getByPrefix(key string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	resp, err := e.client.Get(ctx, key, clientv3.WithPrefix())
	if err != nil {
		cancel()
		return nil, err
	}
	m := make(map[string]string)
	for _, kv := range resp.Kvs {
		m[string(kv.Key)] = string(kv.Value)
	}
	cancel()

	return m, err
}

func (e *Etcd) update(key, value string) error {
	e.liveKeyIDLock.Lock()
	id := e.liveKeyID[key]
	e.liveKeyIDLock.Unlock()
	_, err := e.client.Put(context.TODO(), key, value, clientv3.WithLease(id))
	if err != nil {
		err = e.keep(key, value)
		if err != nil {
			log.Errorf("Etcd.Keep %s %s %v", key, value, err)
		}
	}
	// log.Infof("Etcd.Update %s %s %v", key, value, err)
	return err
}
