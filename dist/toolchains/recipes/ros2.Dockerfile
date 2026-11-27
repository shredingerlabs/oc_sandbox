# ros2 — ROS 2 Jazzy (spec toolchain-sets §3): the vendor apt repo,
# ros-jazzy-ros-base, rviz2, the CycloneDDS RMW, rosdep/colcon/vcstool.
# gnupg is a build-only dep (keyring dearmor) — installed and purged inside
# the same RUN so it never enters the payload. The repo file + keyring +
# rosdep init's source list are root-side /etc writes — author-declared
# extras. The ROS environment line rides the ~/.profile.d hook (direct
# ~/.bashrc writes are lint-refused); the rosdep cache builds as dev into
# /home/dev/.ros (auto-captured).
# extra: /etc/apt/sources.list.d/ros2.list
# extra: /usr/share/keyrings/ros-archive-keyring.gpg
# extra: /etc/ros/rosdep/sources.list.d/20-default.list
USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends gnupg \
    && curl -fsSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key \
        | gpg --dearmor -o /usr/share/keyrings/ros-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] http://packages.ros.org/ros2/ubuntu noble main" \
        > /etc/apt/sources.list.d/ros2.list \
    && apt-get purge -y gnupg && apt-get autoremove -y --purge \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ros-jazzy-ros-base \
        ros-jazzy-rviz2 \
        ros-jazzy-rmw-cyclonedds-cpp \
        python3-rosdep python3-colcon-common-extensions python3-vcstool \
    && rm -rf /var/lib/apt/lists/* \
    && rosdep init
USER dev
RUN mkdir -p ~/.profile.d \
    && printf 'source /opt/ros/jazzy/setup.bash\n' > ~/.profile.d/ros2.sh \
    && rosdep update
