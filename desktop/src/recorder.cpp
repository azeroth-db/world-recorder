#include "recorder.h"

#include <QDir>
#include <QFile>
#include <QJsonDocument>
#include <QNetworkReply>
#include <QNetworkRequest>

using namespace std::chrono_literals;

Recorder::Recorder(const QString &api, const QString &stateDir, QObject *parent)
    : QObject(parent), m_base("http://" + api), m_stateDir(stateDir) {
    m_retry.setSingleShot(true);
    m_retry.setInterval(2s);
    connect(&m_retry, &QTimer::timeout, this, &Recorder::listen);
}

void Recorder::start() { listen(); }

// The program sends its whole status as one event at once and again after every change.
void Recorder::listen() {
    QNetworkRequest request(m_base.resolved(QUrl("/v1/events")));
    request.setAttribute(QNetworkRequest::CacheLoadControlAttribute, QNetworkRequest::AlwaysNetwork);
    m_buffer.clear();
    m_events = m_network.get(request);
    connect(m_events, &QNetworkReply::readyRead, this, &Recorder::read);
    connect(m_events, &QNetworkReply::finished, this, &Recorder::lost);
}

void Recorder::read() {
    m_buffer += m_events->readAll();
    for (qsizetype end; (end = m_buffer.indexOf("\n\n")) >= 0; m_buffer.remove(0, end + 2)) {
        for (const QByteArray &line : m_buffer.left(end).split('\n')) {
            if (!line.startsWith("data: "))
                continue;
            if (QJsonObject status = QJsonDocument::fromJson(line.mid(6)).object(); !status.isEmpty()) {
                m_status = status;
                emit changed();
            }
        }
    }
}

// The stream ends when the program stops or restarts for an update; until it answers again it is not running.
void Recorder::lost() {
    m_events->deleteLater();
    if (!m_status.isEmpty()) {
        m_status = {};
        emit changed();
    }
    m_retry.start();
}

QNetworkReply *Recorder::command(const QString &name, const QJsonObject &body) {
    // Without the token the program refuses the command, and the window says it could not do that.
    QFile token(QDir(m_stateDir).filePath("control-token"));
    QByteArray value = token.open(QIODevice::ReadOnly) ? token.readAll().trimmed() : QByteArray();
    QNetworkRequest request(m_base.resolved(QUrl("/v1/" + name)));
    request.setHeader(QNetworkRequest::ContentTypeHeader, "application/json");
    request.setRawHeader("X-Recorder-Control", value);
    QNetworkReply *reply = m_network.post(request, QJsonDocument(body).toJson(QJsonDocument::Compact));
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        reply->deleteLater();
        int code = reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt();
        // 409 is a command already running or a folder choice the player cancelled.
        if (code >= 400 && code != 409)
            emit failed();
    });
    return reply;
}
