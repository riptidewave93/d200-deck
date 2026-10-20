package main

import (
	"log"

	"github.com/godbus/dbus/v5"
)

const (
	notifications     = "org.freedesktop.Notifications"
	notificationsPath = "/org/freedesktop/Notifications"
)

// notify shows a desktop notification that opens url when clicked.
func notify(title, body, url string) {
	if err := showNotification(title, body, url); err != nil {
		log.Printf("notification: %v", err)
	}
}

func showNotification(title, body, url string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	err = conn.AddMatchSignal(dbus.WithMatchInterface(notifications), dbus.WithMatchObjectPath(notificationsPath))
	if err != nil {
		conn.Close()
		return err
	}
	signals := make(chan *dbus.Signal, 8)
	conn.Signal(signals)
	var id uint32
	err = conn.Object(notifications, notificationsPath).Call(notifications+".Notify", 0,
		"deckhost", uint32(0), "software-update-available", title, body,
		[]string{"default", "Open the release page"}, map[string]dbus.Variant{}, int32(0)).Store(&id)
	if err != nil {
		conn.Close()
		return err
	}
	// a click can come long after, from the notification list
	go func() {
		defer conn.Close()
		for s := range signals {
			if len(s.Body) == 0 || s.Body[0] != id {
				continue
			}
			switch s.Name {
			case notifications + ".ActionInvoked":
				if err := openURL(url); err != nil {
					log.Printf("notification: %v", err)
				}
				return
			case notifications + ".NotificationClosed":
				return
			}
		}
	}()
	return nil
}
