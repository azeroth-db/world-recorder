#pragma once

#include <QByteArray>
#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QObject>
#include <QPointer>
#include <QString>
#include <QTimer>
#include <QUrl>

class QNetworkReply;

// The recorder program's loopback API: its status stream and its commands. Commands carry the control token the
// program writes to its state folder at start.
class Recorder : public QObject {
    Q_OBJECT
public:
    Recorder(const QString &api, const QString &stateDir, QObject *parent = nullptr);
    void start();
    QNetworkReply *command(const QString &name, const QJsonObject &body = {});
    void set(const QString &setting, bool on) { command("settings", {{setting, on}}); }
    const QJsonObject &status() const { return m_status; }
    bool reachable() const { return !m_status.isEmpty(); }

signals:
    void changed();
    void failed();

private:
    void listen();
    void read();
    void lost();

    QNetworkAccessManager m_network;
    QUrl m_base;
    QString m_stateDir;
    QPointer<QNetworkReply> m_events;
    QByteArray m_buffer;
    QJsonObject m_status;
    QTimer m_retry;
};
