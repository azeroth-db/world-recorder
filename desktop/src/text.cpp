#include "text.h"

#include <QDateTime>
#include <QFile>
#include <QJsonDocument>

// The site's language codes; Spanish outside Spain and Chinese written in Traditional characters take their own.
static QString siteLanguage(const QLocale &locale) {
    switch (locale.language()) {
    case QLocale::German: return "deDE";
    case QLocale::French: return "frFR";
    case QLocale::Korean: return "koKR";
    case QLocale::Portuguese: return "ptBR";
    case QLocale::Russian: return "ruRU";
    case QLocale::Spanish: return locale.territory() == QLocale::Spain ? "esES" : "esMX";
    case QLocale::Chinese: return locale.script() == QLocale::TraditionalChineseScript ? "zhTW" : "zhCN";
    default: return "enUS";
    }
}

// The interface language the player chose for the system, not its region format.
Text::Text() : m_locale(QLocale::system().uiLanguages().value(0, "en-US")) {
    QFile file(":/strings.json");
    if (file.open(QIODevice::ReadOnly))
        m_phrases = QJsonDocument::fromJson(file.readAll()).object().value(siteLanguage(m_locale)).toObject();
}

QString Text::operator()(const QString &phrase, std::initializer_list<std::pair<QString, QString>> values) const {
    QString text = m_phrases.value(phrase).toString(phrase);
    for (const auto &[name, value] : values)
        text.replace('{' + name + '}', value);
    return text;
}

QString Text::date(qint64 ms) const {
    return m_locale.toString(QDateTime::fromMSecsSinceEpoch(ms), QLocale::ShortFormat);
}

QString Text::size(qint64 bytes) const {
    return m_locale.formattedDataSize(bytes, 1, QLocale::DataSizeSIFormat);
}
