#pragma once

#include <QJsonObject>
#include <QLocale>
#include <QString>

#include <initializer_list>
#include <utility>

// The app's phrases in the system language, from resources/strings.json (strings.py writes it from the site's own
// interface resources); a phrase it lacks reads in English.
class Text {
public:
    Text();
    QString operator()(const QString &phrase, std::initializer_list<std::pair<QString, QString>> values = {}) const;
    QString date(qint64 ms) const;
    QString size(qint64 bytes) const;
    const QLocale &locale() const { return m_locale; }

private:
    QJsonObject m_phrases;
    QLocale m_locale;
};
