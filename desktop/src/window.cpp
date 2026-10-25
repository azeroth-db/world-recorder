#include "window.h"

#include <QApplication>
#include <QCheckBox>
#include <QCloseEvent>
#include <QDesktopServices>
#include <QGridLayout>
#include <QJsonArray>
#include <QMenu>
#include <QNetworkReply>
#include <QProcess>
#include <QPushButton>
#include <QScrollArea>
#include <QTimer>
#include <QVBoxLayout>

#include <algorithm>

static QString text(const QJsonValue &value) { return value.toString(); }

// Dotted versions compared part by part; a missing version is older than any.
static bool older(const QString &installed, const QString &bundled) {
    if (installed.isEmpty())
        return true;
    const QStringList a = installed.split('.'), b = bundled.split('.');
    for (qsizetype i = 0; i < std::max(a.size(), b.size()); ++i) {
        int x = i < a.size() ? a[i].toInt() : 0, y = i < b.size() ? b[i].toInt() : 0;
        if (x != y)
            return x < y;
    }
    return false;
}

static const QMap<QString, QString> saveState = {{"pending", "Waiting to upload"}, {"failed", "Upload failed"},
    {"unlinked", "Not linked"}, {"refused", "Refused"}, {"invalid", "Cannot be read"}};
static const QMap<QString, QString> sendResult = {{"accepted", "Accepted"}, {"duplicate", "Already uploaded"},
    {"failed", "Failed"}};

Window::Window(const Launch &launch)
    : m_launch(launch), m_recorder(launch.api, launch.stateDir), m_body(new QVBoxLayout), m_error(new QLabel) {
    setWindowTitle(t("World Recorder"));
    setObjectName("frame");
    auto *title = new QLabel(t("World Recorder"));
    title->setObjectName("title");
    m_error->setObjectName("error");
    m_error->hide();
    auto *outer = new QVBoxLayout(this);
    // render() sizes the window to its content.
    outer->setSizeConstraint(QLayout::SetNoConstraint);
    outer->setContentsMargins(28, 22, 28, 24);
    outer->setSpacing(14);
    outer->addWidget(title);
    outer->addLayout(m_body);
    outer->addWidget(m_error);
    outer->addStretch();

    auto *menu = new QMenu(this);
    menu->addAction(t("Open"), this, &Window::present);
    m_send = menu->addAction(t("Upload now"), this, [this] { m_recorder.command("send"); });
    m_pause = menu->addAction(t("Pause"), this, [this] { m_recorder.set("paused", m_pause->isChecked()); });
    m_pause->setCheckable(true);
    menu->addAction(t("Your records"), this, &Window::openRecords);
    menu->addSeparator();
    menu->addAction(t("Close"), qApp, &QApplication::quit);
#ifdef Q_OS_MACOS
    QIcon tray(":/tray-mac.png");
    tray.setIsMask(true);
#else
    QIcon tray(":/icon.png");
#endif
    m_tray.setIcon(tray);
    m_tray.setContextMenu(menu);
    connect(&m_tray, &QSystemTrayIcon::activated, this, [this](QSystemTrayIcon::ActivationReason reason) {
        if (reason == QSystemTrayIcon::Trigger || reason == QSystemTrayIcon::DoubleClick)
            present();
    });
    m_tray.show();

    connect(&m_recorder, &Recorder::changed, this, &Window::render);
    connect(&m_recorder, &Recorder::failed, this, [this] { m_error->show(); });
    render();
    m_recorder.start();
}

void Window::present() {
    show();
    raise();
    activateWindow();
}

// The window hides; the tray icon keeps the app until the player closes it from the tray menu.
void Window::closeEvent(QCloseEvent *event) {
    hide();
    event->ignore();
}

void Window::openRecords() { QDesktopServices::openUrl(QUrl(m_launch.origin + "/account/world-recorder")); }

// Widgets go later: a button's own click can redraw the window.
static void dispose(QLayout *layout) {
    while (QLayoutItem *item = layout->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        } else if (QLayout *inner = item->layout()) {
            dispose(inner);
        }
        delete item;
    }
}

void Window::clear() {
    dispose(m_body);
    m_steps = new QGridLayout;
    m_steps->setHorizontalSpacing(12);
    m_steps->setVerticalSpacing(10);
    m_steps->setColumnStretch(2, 1);
    m_body->addLayout(m_steps);
}

void Window::render() {
    clear();
    m_error->setText(t("World Recorder could not do that."));
    const QJsonObject &status = m_recorder.status();
    if (!m_recorder.reachable())
        renderUnreached();
    else if (status.value("key").isString())
        renderLive(status);
    else
        renderUnlinked(status);
    updateTray();
    QTimer::singleShot(0, this, &Window::fit);
}

// The window is as wide as its widest row needs and at least 640, and as tall as its wrapped text at that width, so
// no line is ever cut.
void Window::fit() {
    layout()->activate();
    int width = std::max(640, layout()->minimumSize().width());
    setFixedSize(width, hasHeightForWidth() ? heightForWidth(width) : layout()->sizeHint().height());
}

void Window::updateTray() {
    const QJsonObject &status = m_recorder.status();
    bool live = m_recorder.reachable();
    m_send->setEnabled(live && status.value("busy").isNull());
    m_pause->setEnabled(live);
    m_pause->setChecked(status.value("paused").toBool());
    m_tray.setToolTip(t("World Recorder") + (live ? "" : "\n" + t("Not running on this computer")));
}

void Window::step(const QString &label, const QString &value, const QString &mark, QWidget *action,
                  const QString &detail) {
    int row = m_steps->rowCount();
    auto *dot = new QLabel;
    dot->setObjectName("mark");
    dot->setProperty("mark", mark);
    dot->setFixedSize(12, 12);
    auto *name = new QLabel(label);
    name->setObjectName("label");
    auto *shown = new QLabel(value);
    shown->setObjectName("value");
    shown->setWordWrap(true);
    shown->setTextInteractionFlags(Qt::TextSelectableByMouse);
    shown->setToolTip(detail);
    m_steps->addWidget(dot, row, 0, Qt::AlignVCenter);
    m_steps->addWidget(name, row, 1, Qt::AlignVCenter);
    m_steps->addWidget(shown, row, 2, Qt::AlignVCenter);
    if (action)
        m_steps->addWidget(action, row, 3, Qt::AlignVCenter);
    if (!detail.isEmpty()) {
        auto *why = new QLabel(detail);
        why->setObjectName("detail");
        why->setWordWrap(true);
        m_steps->addWidget(why, row + 1, 2, 1, 2);
    }
}

QWidget *Window::button(const QString &label, const std::function<void()> &run, bool enabled, const QString &kind) {
    auto *b = new QPushButton(label);
    b->setProperty("kind", kind);
    b->setEnabled(enabled);
    b->setCursor(Qt::PointingHandCursor);
    connect(b, &QPushButton::clicked, this, [this, run] {
        m_error->hide();
        run();
    });
    return b;
}

// A release newer than the running program is a step not done, with the command that installs it.
void Window::programStep(const QJsonObject &status) {
    QString target = status.value("update").toObject().value("version").toString();
    QString busy = text(status.value("busy"));
    QWidget *action = target.isEmpty() ? nullptr
        : button(busy == "update" ? t("Updating…") : t("Update to {version}", {{"version", target}}),
                 [this] { m_recorder.command("update"); }, busy.isEmpty());
    step(t("World Recorder"), text(status.value("version")), target.isEmpty() ? "ready" : "waiting", action);
}

void Window::renderUnreached() {
    step(t("World Recorder"), t("Not running on this computer"), "notready",
         button(t("Install"), [this] { QProcess::startDetached(m_launch.program, {"setup"}); }));
}

void Window::renderUnlinked(const QJsonObject &status) {
    programStep(status);
    bool linking = text(status.value("busy")) == "link";
    step(t("Linked as"), t("Not linked"), "notready",
         button(linking ? t("Linking…") : t("Link this computer"), [this] { m_recorder.command("link"); }, !linking));
}

void Window::renderLive(const QJsonObject &status) {
    bool busy = !status.value("busy").isNull();
    programStep(status);
    step(t("Linked as"), text(status.value("computer")), "ready");

    QJsonObject folder = status.value("folder").toObject(), addon = status.value("addon").toObject();
    bool chosen = folder.value("root").isString() && folder.value("error").isNull();
    // A client World Recorder does not read yet is named in the player's language; any other problem keeps the program's
    // own sentence below the request for the folder.
    QStringList unknown;
    for (const QJsonValue &client : folder.value("unknown").toArray())
        unknown << text(client.toObject().value("version"));
    QString value = chosen ? text(folder.value("root")) : unknown.isEmpty() ? t("Choose the World of Warcraft folder.")
        : t("World Recorder does not read World of Warcraft {version} yet. It updates itself as soon as it can.",
            {{"version", t.locale().createSeparatedList(unknown)}});
    step(t("Game folder"), value, chosen ? "ready" : "notready",
         button(t("Change"), [this] { m_recorder.command("folder"); }, !busy, "secondary"),
         unknown.isEmpty() ? text(folder.value("error")) : QString());

    QString installed = text(addon.value("installed"));
    bool stale = older(installed, text(addon.value("bundled")));
    step(t("AddOns"), installed.isEmpty() ? t("Missing") : installed,
         !stale ? "ready" : installed.isEmpty() ? "notready" : "waiting",
         chosen && stale ? button(installed.isEmpty() ? t("Install") : t("Update"),
                                  [this] { m_recorder.command("addon"); }, !busy, "secondary") : nullptr);

    QJsonArray saves = status.value("saves").toArray();
    qint64 written = 0, uploaded = 0;
    QString unsent, broken;
    for (const QJsonValue &value : saves) {
        QJsonObject save = value.toObject();
        written = std::max(written, save.value("writtenAt").toInteger());
        uploaded = std::max(uploaded, save.value("sentAt").toInteger());
        QString state = text(save.value("state"));
        if (state != "sent")
            unsent = state;
        if (state != "sent" && state != "pending")
            broken = state;
    }
    step(t("Saved records"), written ? t("Saved {date}", {{"date", t.date(written)}})
                                     : t("Log out, or type /reload or /wrsave in game."), written ? "ready" : "waiting");
    if (status.value("paused").toBool())
        step(t("Upload"), t("Paused"), "waiting");
    else if (!broken.isEmpty())
        step(t("Upload"), t(saveState.value(broken)), "notready");
    else if (!unsent.isEmpty() || saves.isEmpty())
        step(t("Upload"), t("Waiting to upload"), "waiting");
    else
        step(t("Upload"), t("Uploaded {date}", {{"date", t.date(uploaded)}}), "ready");

    settings(status);
    history(status.value("sends").toArray());
    removal(busy);
}

void Window::settings(const QJsonObject &status) {
    bool busy = !status.value("busy").isNull();
    auto *row = new QHBoxLayout;
    row->setSpacing(22);
    row->addWidget(button(t("Upload now"), [this] { m_recorder.command("send"); }, !busy));
    for (auto [name, label] : {std::pair{"paused", "Pause"}, {"autoUpdate", "Update automatically"},
                               {"startAtLogin", "Start at login"}}) {
        auto *box = new QCheckBox(t(label));
        box->setChecked(status.value(name).toBool());
        box->setEnabled(!busy);
        box->setCursor(Qt::PointingHandCursor);
        connect(box, &QCheckBox::toggled, this, [this, setting = QString(name)](bool on) { m_recorder.set(setting, on); });
        row->addWidget(box);
    }
    row->addStretch();
    m_body->addLayout(row);
}

void Window::history(const QJsonArray &sends) {
    if (sends.isEmpty())
        return;
    auto *heading = new QLabel(t("Upload history"));
    heading->setObjectName("heading");
    m_body->addWidget(heading);
    auto *list = new QWidget;
    auto *rows = new QGridLayout(list);
    rows->setContentsMargins(10, 8, 10, 8);
    rows->setColumnStretch(0, 1);
    for (qsizetype i = 0; i < sends.size(); ++i) {
        QJsonObject send = sends[i].toObject();
        QString result = text(send.value("result"));
        auto *outcome = new QLabel(t(sendResult.value(result, result)));
        outcome->setProperty("result", result);
        rows->addWidget(new QLabel(t.date(send.value("at").toInteger())), int(i), 0);
        rows->addWidget(new QLabel(t.size(send.value("bytes").toInteger())), int(i), 1, Qt::AlignRight);
        rows->addWidget(outcome, int(i), 2, Qt::AlignRight);
    }
    auto *scroll = new QScrollArea;
    scroll->setObjectName("history");
    scroll->setWidget(list);
    scroll->setWidgetResizable(true);
    scroll->setFrameShape(QFrame::NoFrame);
    scroll->setMaximumHeight(170);
    m_body->addWidget(scroll);
}

void Window::removal(bool busy) {
    auto *row = new QHBoxLayout;
    row->addWidget(button(t("Your records"), [this] { openRecords(); }, true, "secondary"));
    if (m_asking) {
        // The question wraps rather than widening the window.
        auto *question = new QLabel(t("Remove World Recorder from this computer?"));
        question->setWordWrap(true);
        question->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        row->addWidget(question, 1);
        row->addWidget(button(t("Remove from this computer"), [this] {
            // The program removes itself once it has the request; the app goes when it answers.
            connect(m_recorder.command("uninstall"), &QNetworkReply::finished, qApp, &QApplication::quit);
        }, !busy));
        row->addWidget(button(t("Keep"), [this] { m_asking = false; render(); }, true, "secondary"));
    } else {
        row->addStretch();
        row->addWidget(button(t("Remove from this computer"), [this] { m_asking = true; render(); }, !busy, "quiet"));
    }
    m_body->addLayout(row);
}
